package v2beta1

import (
	"net/http"
	"net/url"
	"testing"

	innerReq "github.com/emqx/emqx-operator/internal/requester"
	"github.com/stretchr/testify/require"
)

func TestComputeDashboardAdminDigest(t *testing.T) {
	hash1 := computeDashboardAdminDigest("admin", []byte("secret"), "1")
	hash2 := computeDashboardAdminDigest("admin", []byte("secret"), "1")
	hash3 := computeDashboardAdminDigest("emqx", []byte("secret"), "1")
	hash4 := computeDashboardAdminDigest("admin", []byte("secret"), "2")

	require.Equal(t, hash1, hash2)
	require.NotEqual(t, hash1, hash3)
	require.NotEqual(t, hash1, hash4)
}

func TestSanitizeCommand(t *testing.T) {
	require.Equal(t, "emqx_ctl admins passwd admin ****", sanitizeCommand([]string{"emqx_ctl", "admins", "passwd", "admin", "secret"}))
	require.Equal(t, "sh -c echo", sanitizeCommand([]string{"sh", "-c", "echo"}))
	require.Equal(t, "", sanitizeCommand(nil))
}

func TestSyncDashboardAdminByAPISuccess(t *testing.T) {
	fake := &innerReq.FakeRequester{
		ReqFunc: func(method string, url url.URL, body []byte, header http.Header) (*http.Response, []byte, error) {
			return &http.Response{StatusCode: http.StatusOK, Status: http.StatusText(http.StatusOK)}, []byte(`{}`), nil
		},
	}

	require.NoError(t, syncDashboardAdminByAPI(fake, "admin", "secret", false))
}

func TestSyncDashboardAdminByAPICreate(t *testing.T) {
	var calls int
	fake := &innerReq.FakeRequester{
		ReqFunc: func(method string, url url.URL, body []byte, header http.Header) (*http.Response, []byte, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusNotFound, Status: http.StatusText(http.StatusNotFound)}, []byte(`{}`), nil
			}
			return &http.Response{StatusCode: http.StatusCreated, Status: http.StatusText(http.StatusCreated)}, []byte(`{}`), nil
		},
	}

	require.NoError(t, syncDashboardAdminByAPI(fake, "admin", "secret", true))
	require.Equal(t, 2, calls)
}

func TestSyncDashboardAdminByAPIFail(t *testing.T) {
	fake := &innerReq.FakeRequester{
		ReqFunc: func(method string, url url.URL, body []byte, header http.Header) (*http.Response, []byte, error) {
			return &http.Response{StatusCode: http.StatusInternalServerError, Status: http.StatusText(http.StatusInternalServerError)}, []byte(`oops`), nil
		},
	}

	require.Error(t, syncDashboardAdminByAPI(fake, "admin", "secret", false))
}
