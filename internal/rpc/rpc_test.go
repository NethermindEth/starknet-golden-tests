package rpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NethermindEth/starknet-golden-tests/internal/rpc"
)

func TestSplit(t *testing.T) {
	t.Run("a single response belongs to the request whatever its id", func(t *testing.T) {
		got, err := rpc.Split([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`), false, 7)
		require.NoError(t, err)
		assert.JSONEq(t, `{"code":-32700,"message":"Parse error"}`, string(got[7].Error))
	})

	t.Run("a batch splits by id", func(t *testing.T) {
		got, err := rpc.Split([]byte(`[{"id":2,"result":"b"},{"id":1,"result":"a"}]`), true, 0)
		require.NoError(t, err)
		assert.Equal(t, `"a"`, string(got[1].Result))
		assert.Equal(t, `"b"`, string(got[2].Result))
	})
}

func TestPostRetries(t *testing.T) {
	for _, tt := range []struct {
		status int
		calls  int64
	}{
		{status: http.StatusUnauthorized, calls: 1},
		{status: http.StatusBadGateway, calls: 4},
		{status: http.StatusTooManyRequests, calls: 4},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			var calls atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()
			client := rpc.New(srv.URL, 1)
			client.RetryBackoff = time.Millisecond
			_, err := client.Post(context.Background(), []byte(`{}`))
			require.ErrorContains(t, err, http.StatusText(tt.status))
			assert.Equal(t, tt.calls, calls.Load())
		})
	}
}

func TestPostWaitsForRetryAfter(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"result":1}`))
	}))
	defer srv.Close()
	client := rpc.New(srv.URL, 1)
	client.RetryBackoff = time.Millisecond
	start := time.Now()
	_, err := client.Post(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), time.Second)
}

func TestPostDoesNotRetryAnInvalidURL(t *testing.T) {
	start := time.Now()
	_, err := rpc.New("ftp://user:secret@node.example/secret", 1).Post(context.Background(), []byte(`{}`))
	require.ErrorContains(t, err, "node URL is not valid: ftp://node.example")
	assert.NotContains(t, err.Error(), "secret")
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestPostShortensTheErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(strings.Repeat("x", 1000) + "secret"))
	}))
	defer srv.Close()
	_, err := rpc.New(srv.URL, 1).Post(context.Background(), []byte(`{}`))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	assert.Less(t, len(err.Error()), 300)
}

func TestRedact(t *testing.T) {
	for _, raw := range []string{
		"https://user:secret@node.example:6060/rpc/secret?apikey=secret",
		"https://node.example:6060/v2/secret",
	} {
		assert.Equal(t, "https://node.example:6060", rpc.Redact(raw))
	}
	assert.Equal(t, "<node URL>", rpc.Redact("::secret"))
}
