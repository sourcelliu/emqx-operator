package v2beta1

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	emperror "emperror.dev/errors"
	appsv2beta1 "github.com/emqx/emqx-operator/apis/apps/v2beta1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func (r *EMQXReconciler) execEmqxCommand(ctx context.Context, logger logr.Logger, pod *corev1.Pod, command []string) error {
	_, _, err := r.execEmqxCommandWithResult(ctx, logger, pod, command)
	return err
}

func (r *EMQXReconciler) execEmqxCommandWithResult(ctx context.Context, logger logr.Logger, pod *corev1.Pod, command []string) (string, string, error) {
	if node, err := r.Clientset.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{}); err == nil {
		address := ""
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				address = addr.Address
				break
			}
		}
		if len(address) > 0 {
			if stdout, stderr, kubeletErr := r.execEmqxCommandViaKubelet(ctx, pod, command, address, "10250"); kubeletErr == nil {
				logger.Info("execEmqxCommandViaKubelet success.")
				return stdout, stderr, nil
			} else {
				logger.Info("execEmqxCommandViaKubelet err.", "reason", kubeletErr)
			}
		}
	} else {
		logger.Info("failed to get node.", "reason", err)
	}

	req := r.Clientset.CoreV1().RESTClient().Post().
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

	exec, err := remotecommand.NewSPDYExecutor(r.Config, http.MethodPost, req.URL())
	if err != nil {
		return "", "", emperror.Wrap(err, "failed to initialize exec session")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		if host, port, ok := extractAllIPPorts(err.Error()); ok {
			if out, errOut, kubeletErr := r.execEmqxCommandViaKubelet(ctx, pod, command, host, port); kubeletErr != nil {
				return "", "", emperror.Wrapf(kubeletErr, "command %s failed via kubelet fallback", sanitizeCommand(command))
			} else {
				return out, errOut, nil
			}
		}
		return "", "", emperror.Wrapf(err, "command %s failed: %s", sanitizeCommand(command), strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), stderr.String(), nil
}

func (r *EMQXReconciler) execEmqxCommandViaKubelet(ctx context.Context, pod *corev1.Pod, command []string, host, port string) (string, string, error) {
	token, err := loadOperatorToken(r.Config)
	if err != nil {
		return "", "", emperror.Wrap(err, "failed to load operator token")
	}

	kubeletURL := buildKubeletExecURL(pod, host, port, command)

	configCopy := restclient.CopyConfig(r.Config)
	configCopy.Host = fmt.Sprintf("https://%s", net.JoinHostPort(host, port))
	configCopy.BearerToken = token
	configCopy.BearerTokenFile = ""
	configCopy.TLSClientConfig = restclient.TLSClientConfig{
		Insecure: true,
	}

	exec, err := remotecommand.NewSPDYExecutor(configCopy, http.MethodPost, kubeletURL)
	if err != nil {
		return "", "", emperror.Wrap(err, "failed to initialize kubelet exec session")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return "", "", emperror.Wrapf(err, "command %s via kubelet failed: %s", sanitizeCommand(command), strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), stderr.String(), nil
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
	pattern := `\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]{1,5}\b`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllString(input, -1)
	for _, match := range matches {
		cleaned := strings.TrimRight(match, ":")
		if host, port, ok := validateIPPort(cleaned); ok {
			return host, port, true
		}
	}
	return "", "", false
}

func extractDialTimeoutAddress(err error) (string, string, bool) {
	if err == nil {
		return "", "", false
	}
	return extractAllIPPorts(err.Error())
}

func validateIPPort(addr string) (string, string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", false
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return "", "", false
	}
	if ip := net.ParseIP(host); ip != nil {
		return host, port, true
	}
	if host != "" && len(host) <= 253 {
		return host, port, true
	}
	return host, port, false
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
