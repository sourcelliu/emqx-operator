/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v2beta1

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	appsv2beta1 "github.com/emqx/emqx-operator/apis/apps/v2beta1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

// var cfg *rest.Config
var timeout, interval time.Duration
var testEnv *envtest.Environment

var k8sClient client.Client
var logger logr.Logger
var ctx context.Context

var emqxReconciler *EMQXReconciler
var emqx *appsv2beta1.EMQX = &appsv2beta1.EMQX{
	ObjectMeta: metav1.ObjectMeta{
		UID:  "fake-1234567890",
		Name: "emqx",
		Labels: map[string]string{
			appsv2beta1.LabelsManagedByKey: "emqx-operator",
			appsv2beta1.LabelsInstanceKey:  "emqx",
		},
	},
	Spec: appsv2beta1.EMQXSpec{
		Image: "emqx",
	},
}

var requiredEnvtestBinaries = []string{"kube-apiserver", "etcd", "kubectl"}

const (
	envtestIndexURL        = "https://raw.githubusercontent.com/kubernetes-sigs/controller-tools/HEAD/envtest-releases.yaml"
	envtestDownloadTimeout = 2 * time.Minute
	envtestDefaultVersion  = "v1.30.0"
)

var errEnvtestUnavailable = errors.New("envtest assets unavailable")

type envtestIndex struct {
	Releases map[string]map[string]envtestArchive `yaml:"releases"`
}

type envtestArchive struct {
	Hash     string `yaml:"hash"`
	SelfLink string `yaml:"selfLink"`
}

type versionCandidate struct {
	key string
	ver *semver.Version
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

func fallbackArchiveURL(version, goos, arch string) string {
	version = normalizeVersion(version)
	return fmt.Sprintf("https://github.com/kubernetes-sigs/controller-tools/releases/download/envtest-%s/envtest-%s-%s-%s.tar.gz", version, version, goos, arch)
}

func hasEnvtestBinaries(dir string) bool {
	if dir == "" {
		return false
	}
	for _, bin := range requiredEnvtestBinaries {
		if _, err := os.Stat(filepath.Join(dir, bin)); err != nil {
			return false
		}
	}
	return true
}

func findCachedEnvtestDir(root, requestedVersion, goos, goarch string) (string, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}

	requestedVersion = strings.TrimPrefix(requestedVersion, "v")
	type candidate struct {
		path    string
		version *semver.Version
	}

	var (
		candidates []candidate
		fallback   string
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		parts := strings.Split(entry.Name(), "-")
		if len(parts) < 3 {
			continue
		}
		dirVersion := strings.TrimPrefix(parts[0], "v")
		dirOS := parts[len(parts)-2]
		dirArch := parts[len(parts)-1]

		if dirOS != goos || dirArch != goarch {
			continue
		}

		if requestedVersion != "" && dirVersion != requestedVersion {
			continue
		}

		path := filepath.Join(root, entry.Name())
		if !hasEnvtestBinaries(path) {
			continue
		}

		v, err := semver.NewVersion(dirVersion)
		if err != nil {
			if requestedVersion != "" {
				return path, true
			}
			if fallback == "" {
				fallback = path
			}
			continue
		}
		candidates = append(candidates, candidate{path: path, version: v})
	}

	if requestedVersion != "" {
		if len(candidates) > 0 {
			return candidates[0].path, true
		}
		return "", false
	}

	if len(candidates) == 0 {
		if fallback != "" {
			return fallback, true
		}
		return "", false
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].version.GreaterThan(candidates[j].version)
	})
	return candidates[0].path, true
}

func ensureEnvtestBinaries(ctx context.Context, baseLog logr.Logger) (string, error) {
	if existing := os.Getenv("KUBEBUILDER_ASSETS"); hasEnvtestBinaries(existing) {
		baseLog.V(1).Info("using existing KUBEBUILDER_ASSETS", "path", existing)
		return existing, nil
	}

	ctx, cancel := context.WithTimeout(ctx, envtestDownloadTimeout)
	defer cancel()

	assetsRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "testbin", "envtest"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(assetsRoot, 0o755); err != nil {
		return "", err
	}

	requestedVersion := strings.TrimSpace(os.Getenv("ENVTEST_K8S_VERSION"))
	skipAllowed := requestedVersion == ""
	if requestedVersion != "" {
		if cached, ok := findCachedEnvtestDir(assetsRoot, requestedVersion, runtime.GOOS, runtime.GOARCH); ok {
			baseLog.V(1).Info("using cached envtest binaries", "path", cached, "version", requestedVersion)
			return cached, nil
		}
	} else {
		if cached, ok := findCachedEnvtestDir(assetsRoot, envtestDefaultVersion, runtime.GOOS, runtime.GOARCH); ok {
			baseLog.V(1).Info("using cached envtest binaries", "path", cached, "version", envtestDefaultVersion)
			return cached, nil
		}
		if cached, ok := findCachedEnvtestDir(assetsRoot, "", runtime.GOOS, runtime.GOARCH); ok {
			baseLog.V(1).Info("using cached envtest binaries", "path", cached)
			return cached, nil
		}
	}

	index, err := fetchEnvtestIndex(ctx)
	if err != nil {
		baseLog.V(1).Info("failed to fetch envtest index, using fallback version", "error", err)
		index = nil
	}

	versionToFetch := normalizeVersion(requestedVersion)
	if versionToFetch == "" {
		versionToFetch = envtestDefaultVersion
	}

	var (
		archive    envtestArchive
		versionKey string
	)

	if index != nil {
		archive, versionKey, err = selectEnvtestArchive(index, versionToFetch, runtime.GOOS, runtime.GOARCH)
		if err != nil && requestedVersion == "" && versionToFetch != "" {
			baseLog.V(1).Info("preferred envtest version unavailable, falling back to latest", "preferred", versionToFetch)
			archive, versionKey, err = selectEnvtestArchive(index, "", runtime.GOOS, runtime.GOARCH)
		}
		if err != nil {
			if skipAllowed {
				return "", fmt.Errorf("%w: %v", errEnvtestUnavailable, err)
			}
			return "", err
		}
	} else {
		versionKey = versionToFetch
		if versionKey == "" {
			versionKey = envtestDefaultVersion
		}
		archive = envtestArchive{
			SelfLink: fallbackArchiveURL(versionKey, runtime.GOOS, runtime.GOARCH),
		}
	}

	targetDir := filepath.Join(assetsRoot, fmt.Sprintf("%s-%s-%s", versionKey, runtime.GOOS, runtime.GOARCH))
	if hasEnvtestBinaries(targetDir) {
		baseLog.V(1).Info("reusing cached envtest binaries", "path", targetDir, "version", versionKey)
		return targetDir, nil
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp("", "envtest-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	baseLog.Info("downloading envtest binaries", "version", versionKey, "url", archive.SelfLink)
	if err := downloadToFile(ctx, archive.SelfLink, tmpFile); err != nil {
		if skipAllowed {
			return "", fmt.Errorf("%w: %v", errEnvtestUnavailable, err)
		}
		return "", err
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if err := extractTarGz(tmpFile, targetDir); err != nil {
		return "", err
	}

	if !hasEnvtestBinaries(targetDir) {
		err := fmt.Errorf("envtest binaries missing in %s", targetDir)
		if skipAllowed {
			return "", fmt.Errorf("%w: %v", errEnvtestUnavailable, err)
		}
		return "", err
	}

	return targetDir, nil
}

func fetchEnvtestIndex(ctx context.Context) (*envtestIndex, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, envtestIndexURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch envtest index: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	index := &envtestIndex{}
	if err := yaml.Unmarshal(body, index); err != nil {
		return nil, err
	}
	return index, nil
}

func selectEnvtestArchive(index *envtestIndex, requestedVersion, osName, arch string) (envtestArchive, string, error) {
	if len(index.Releases) == 0 {
		return envtestArchive{}, "", errors.New("envtest index contains no releases")
	}

	entries := make([]versionCandidate, 0, len(index.Releases))
	for key := range index.Releases {
		v, err := semver.NewVersion(strings.TrimPrefix(key, "v"))
		if err != nil {
			continue
		}
		entries = append(entries, versionCandidate{key: key, ver: v})
	}

	if len(entries) == 0 {
		return envtestArchive{}, "", errors.New("no valid versions found in envtest index")
	}

	if requestedVersion != "" {
		targetVersion, err := semver.NewVersion(strings.TrimPrefix(requestedVersion, "v"))
		if err != nil {
			return envtestArchive{}, "", fmt.Errorf("invalid envtest version %q: %w", requestedVersion, err)
		}
		var matched *versionCandidate
		for _, entry := range entries {
			if entry.ver.Equal(targetVersion) {
				matched = &entry
				break
			}
		}
		if matched == nil {
			return envtestArchive{}, "", fmt.Errorf("requested envtest version %q not found", requestedVersion)
		}
		entries = []versionCandidate{*matched}
	} else {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].ver.GreaterThan(entries[j].ver)
		})
	}

	assetName := func(versionKey string) string {
		return fmt.Sprintf("envtest-%s-%s-%s.tar.gz", versionKey, osName, arch)
	}

	for _, entry := range entries {
		name := assetName(entry.key)
		if archive, ok := index.Releases[entry.key][name]; ok {
			return archive, entry.key, nil
		}
	}

	return envtestArchive{}, "", fmt.Errorf("no envtest archive found for %s/%s", osName, arch)
}

func downloadToFile(ctx context.Context, url string, out *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: %s", url, resp.Status)
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}

	return nil
}

func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		filename := filepath.Base(header.Name)
		if filename == "" || filename == "." {
			continue
		}

		targetPath := filepath.Join(dest, filename)
		mode := os.FileMode(header.Mode)
		if mode&0o111 == 0 {
			mode |= 0o755
		}

		if err := writeTarFile(tr, targetPath, mode); err != nil {
			return err
		}
	}
	return nil
}

func writeTarFile(src io.Reader, path string, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return err
	}
	return nil
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	// fetch the current config
	suiteConfig, reporterConfig := GinkgoConfiguration()
	// adjust it
	suiteConfig.SkipStrings = []string{"NEVER-RUN"}
	reporterConfig.FullTrace = true
	// pass it in to RunSpecs
	RunSpecs(t, "Controller Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	opts := zap.Options{
		Development: true,
		Level:       zapcore.DebugLevel,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
		DestWriter:  GinkgoWriter,
	}
	logf.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	timeout = time.Second * 10
	interval = time.Second

	baseLogger := logf.Log.WithName("test")
	ctx = logr.NewContext(context.Background(), baseLogger)
	logger = baseLogger

	Expect(os.Setenv("USE_EXISTING_CLUSTER", "false")).To(Succeed())

	assetsDir, err := ensureEnvtestBinaries(ctx, logf.Log.WithName("envtest-setup"))
	if errors.Is(err, errEnvtestUnavailable) {
		Skip(fmt.Sprintf("skipping controller tests: %v", err))
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(os.Setenv("KUBEBUILDER_ASSETS", assetsDir)).To(Succeed())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: assetsDir,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = appsv2beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).ToNot(HaveOccurred())

	emqxReconciler = NewEMQXReconciler(k8sManager)
	// err = NewEMQXReconciler(k8sManager).SetupWithManager(k8sManager)
	// Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if testEnv != nil {
		_ = testEnv.Stop()
	}
	// Expect(err).NotTo(HaveOccurred())
})
