package v2beta1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	emperror "emperror.dev/errors"
	appsv2beta1 "github.com/emqx/emqx-operator/apis/apps/v2beta1"
	innerReq "github.com/emqx/emqx-operator/internal/requester"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type syncDashboardAdmin struct {
	*EMQXReconciler
}

func (s *syncDashboardAdmin) reconcile(ctx context.Context, logger logr.Logger, instance *appsv2beta1.EMQX, r innerReq.RequesterInterface) subResult {
	spec := instance.Spec.DashboardAdmin
	if spec == nil || spec.PasswordSecretRef == nil {
		if instance.Status.DashboardAdmin != nil {
			instance.Status.DashboardAdmin = nil
		}
		return subResult{}
	}

	username := spec.Username
	if username == "" {
		username = "admin"
	}

	if spec.PasswordSecretRef.Name == "" {
		return subResult{err: emperror.New("dashboardAdmin.passwordSecretRef.name must be set")}
	}
	if spec.PasswordSecretRef.Key == "" {
		return subResult{err: emperror.New("dashboardAdmin.passwordSecretRef.key must be set")}
	}

	passwordSecret := &corev1.Secret{}
	if err := s.Client.Get(ctx, types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      spec.PasswordSecretRef.Name,
	}, passwordSecret); err != nil {
		return subResult{err: emperror.Wrap(err, "failed to get dashboard admin password secret")}
	}

	passwordBytes, ok := passwordSecret.Data[spec.PasswordSecretRef.Key]
	if !ok {
		return subResult{err: emperror.Errorf("secret %s/%s does not contain key %s", passwordSecret.Namespace, passwordSecret.Name, spec.PasswordSecretRef.Key)}
	}
	if len(passwordBytes) == 0 {
		return subResult{err: emperror.Errorf("secret %s/%s key %s is empty", passwordSecret.Namespace, passwordSecret.Name, spec.PasswordSecretRef.Key)}
	}

	digest := computeDashboardAdminDigest(username, passwordBytes, passwordSecret.ResourceVersion)
	if current := instance.Status.DashboardAdmin; current != nil {
		if current.Username == username &&
			current.PasswordHash == digest &&
			current.SecretResourceVersion == passwordSecret.ResourceVersion {
			return subResult{}
		}
	}

	if r == nil {
		return subResult{}
	}

	if !instance.Status.IsConditionTrue(appsv2beta1.CoreNodesReady) {
		return subResult{}
	}

	password := string(passwordBytes)
	if err := s.ensureDashboardAdmin(ctx, logger, instance, r, username, password, spec.CreateIfMissing); err != nil {
		return subResult{err: err}
	}

	if instance.Status.DashboardAdmin == nil {
		instance.Status.DashboardAdmin = &appsv2beta1.DashboardAdminStatus{}
	}
	instance.Status.DashboardAdmin.Username = username
	instance.Status.DashboardAdmin.PasswordHash = digest
	instance.Status.DashboardAdmin.SecretResourceVersion = passwordSecret.ResourceVersion
	instance.Status.DashboardAdmin.LastSynced = metav1.Now()

	s.EventRecorder.Event(instance, corev1.EventTypeNormal, "DashboardAdminPasswordReset", "Dashboard admin credentials reconciled")

	return subResult{}
}

func (s *syncDashboardAdmin) ensureDashboardAdmin(ctx context.Context, logger logr.Logger, instance *appsv2beta1.EMQX, r innerReq.RequesterInterface, username, password string, createIfMissing bool) error {
	return s.syncDashboardAdminWithCtl(ctx, instance, username, password, createIfMissing)
}

func (s *syncDashboardAdmin) syncDashboardAdminWithCtl(ctx context.Context, instance *appsv2beta1.EMQX, username, password string, createIfMissing bool) error {
	pod, err := s.pickReadyCorePod(ctx, instance)
	if err != nil {
		return emperror.Wrap(err, "failed to select ready EMQX pod for password reset")
	}
	if pod == nil {
		return emperror.Errorf("no ready EMQX core pod available for password reset")
	}
	cmd := []string{"emqx_ctl", "admins", "passwd", username, password}
	if err := s.execEmqxCommand(ctx, pod, cmd); err != nil {
		return emperror.Wrap(err, "failed to reset dashboard admin via emqx_ctl")
	}
	return nil
}

func (s *syncDashboardAdmin) pickReadyCorePod(ctx context.Context, instance *appsv2beta1.EMQX) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := s.Client.List(ctx, podList,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels(appsv2beta1.DefaultCoreLabels(instance)),
	); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, nil
	}

	sort.Slice(podList.Items, func(i, j int) bool {
		return podList.Items[i].CreationTimestamp.Before(&podList.Items[j].CreationTimestamp)
	})

	for _, pod := range podList.Items {
		if pod.GetDeletionTimestamp() != nil {
			continue
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.ContainersReady && cond.Status == corev1.ConditionTrue {
				copy := pod.DeepCopy()
				return copy, nil
			}
		}
	}
	return nil, nil
}

func (s *syncDashboardAdmin) execEmqxCommand(ctx context.Context, pod *corev1.Pod, command []string) error {
	req := s.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: appsv2beta1.DefaultContainerName,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, k8sscheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(s.Config, http.MethodPost, req.URL())
	if err != nil {
		return emperror.Wrap(err, "failed to initialize exec session")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		if host, port, ok := extractAllIPPorts(err.Error()); ok {
			if kubeletErr := s.execEmqxCommandViaKubelet(ctx, pod, command, host, port); kubeletErr != nil {
				return emperror.Wrapf(kubeletErr, "command %s failed via kubelet fallback", sanitizeCommand(command))
			}
			return nil
		}
		return emperror.Wrapf(err, "command %s failed: %s", sanitizeCommand(command), strings.TrimSpace(stderr.String()))
	}

	return nil
}

func (s *syncDashboardAdmin) execEmqxCommandViaKubelet(ctx context.Context, pod *corev1.Pod, command []string, host, port string) error {
	token, err := loadOperatorToken(s.Config)
	if err != nil {
		return emperror.Wrap(err, "failed to load operator token")
	}

	kubeletURL := buildKubeletExecURL(pod, host, port, command)

	configCopy := restclient.CopyConfig(s.Config)
	configCopy.Host = fmt.Sprintf("https://%s", net.JoinHostPort(host, port))
	configCopy.BearerToken = token
	configCopy.BearerTokenFile = ""
	configCopy.TLSClientConfig = restclient.TLSClientConfig{
		Insecure: true,
	}

	exec, err := remotecommand.NewSPDYExecutor(configCopy, http.MethodPost, kubeletURL)
	if err != nil {
		return emperror.Wrap(err, "failed to initialize kubelet exec session")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return emperror.Wrapf(err, "command %s via kubelet failed: %s", sanitizeCommand(command), strings.TrimSpace(stderr.String()))
	}

	return nil
}

func buildKubeletExecURL(pod *corev1.Pod, host, port string, command []string) *neturl.URL {
	kubeletHost := net.JoinHostPort(host, port)
	execPath := fmt.Sprintf("/exec/%s/%s/%s",
		neturl.PathEscape(pod.Namespace),
		neturl.PathEscape(pod.Name),
		neturl.PathEscape(appsv2beta1.DefaultContainerName),
	)
	query := neturl.Values{}
	for _, arg := range command {
		query.Add(corev1.ExecCommandParam, arg)
	}
	query.Set(corev1.ExecStdoutParam, "1")
	query.Set(corev1.ExecStderrParam, "1")
	query.Set(corev1.ExecStdinParam, "0")
	query.Set(corev1.ExecTTYParam, "0")

	return &neturl.URL{
		Scheme:   "https",
		Host:     kubeletHost,
		Path:     execPath,
		RawQuery: query.Encode(),
	}
}

func loadOperatorToken(config *restclient.Config) (string, error) {
	if config == nil {
		return "", emperror.New("rest config is not available")
	}
	if token := strings.TrimSpace(config.BearerToken); token != "" {
		return token, nil
	}
	if config.BearerTokenFile != "" {
		data, err := os.ReadFile(config.BearerTokenFile)
		if err != nil {
			return "", err
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", emperror.Errorf("operator token file %s is empty", config.BearerTokenFile)
		}
		return token, nil
	}
	return "", emperror.New("operator token is unavailable in rest config")
}

func extractAllIPPorts(input string) (string, string, bool) {
	// 更全面的正则表达式
	pattern := `\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]{1,5}\b`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllString(input, -1)
	for _, match := range matches {
		// 验证是否是有效的地址:端口
		if host, port, valid := validateIPPort(match); valid {
			return host, port, valid
		}
	}
	return "", "", false
}

func validateIPPort(addr string) (string, string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", false
	}
	// 验证端口
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return "", "", false
	}
	// 验证 IP 地址
	if ip := net.ParseIP(host); ip != nil {
		return host, port, true
	}
	// 如果是主机名，至少要有有效字符
	if host != "" && len(host) <= 253 {
		return host, port, true
	}
	return host, port, false
}

func computeDashboardAdminDigest(username string, password []byte, resourceVersion string) string {
	sum := sha256.Sum256([]byte(username + ":" + string(password) + ":" + resourceVersion))
	return hex.EncodeToString(sum[:])
}

func sanitizeCommand(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	copied := make([]string, len(cmd))
	copy(copied, cmd)
	if len(copied) >= 4 && copied[0] == "emqx_ctl" && copied[1] == "admins" {
		copied[len(copied)-1] = "****"
	}
	return strings.Join(copied, " ")
}
