package authentication

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

func TestConfiguration(t *testing.T) {
	t.Setenv("FIREBASE_PROJECT_ID", "")
	_, err := NewClient(context.Background())
	require.ErrorContains(t, err, "FIREBASE_PROJECT_ID")
	t.Setenv("FIREBASE_PROJECT_ID", "home-inventory-11")
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")
	t.Setenv("APP_ENV", "production")
	_, err = NewClient(context.Background())
	require.ErrorContains(t, err, "APP_ENV=local")
	t.Setenv("APP_ENV", "local")
	t.Setenv("K_SERVICE", "home-inventory-backend-us")
	_, err = NewClient(context.Background())
	require.ErrorContains(t, err, "outside Cloud Run")
}

func TestLocalFirebaseEmulatorInitialization(t *testing.T) {
	t.Setenv("FIREBASE_PROJECT_ID", "local-project")
	t.Setenv("APP_ENV", "local")
	t.Setenv("K_SERVICE", "")
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "127.0.0.1:9099")
	client, err := NewClient(context.Background())
	require.NoError(t, err)
	require.NotNil(t, client)
}

// Exercise the real Admin SDK's cryptographic verification and user lookup.
// All HTTPS traffic is redirected to this fixture; no cloud credentials are used.
func TestSDKTokenVerification(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	publicDER, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	require.NoError(t, err)
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: publicDER}))
	uid := "firebase-non-uuid-identity"
	now := time.Now()
	disabled, deleted := false, false
	validSince := now.Add(-time.Hour).Unix()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com":
			w.Header().Set("Cache-Control", "max-age=3600")
			json.NewEncoder(w).Encode(map[string]string{"test-key": publicPEM})
		case "/v1/projects/home-inventory-11/accounts:lookup":
			if deleted {
				fmt.Fprint(w, `{"users":[]}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"users": []interface{}{
				map[string]interface{}{"localId": uid, "email": "user@example.com", "disabled": disabled, "validSince": strconv.FormatInt(validSince, 10)},
			}})
		default:
			t.Errorf("unexpected SDK request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	previousTransport := http.DefaultTransport
	transport := &http.Transport{DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "www.googleapis.com:443" && addr != "identitytoolkit.googleapis.com:443" {
			return nil, fmt.Errorf("unexpected SDK host: %s", addr)
		}
		return (&tls.Dialer{Config: server.Client().Transport.(*http.Transport).TLSClientConfig}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport; transport.CloseIdleConnections() })
	client, err := newClient(context.Background(), "home-inventory-11", option.WithoutAuthentication())
	require.NoError(t, err)
	sign := func(changes jwt.MapClaims, signingKey *rsa.PrivateKey) string {
		claims := jwt.MapClaims{"sub": uid, "aud": "home-inventory-11", "iss": "https://securetoken.google.com/home-inventory-11", "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(), "email": "user@example.com"}
		for k, v := range changes {
			claims[k] = v
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "test-key"
		encoded, err := token.SignedString(signingKey)
		require.NoError(t, err)
		return encoded
	}
	valid := sign(nil, key)
	token, err := client.VerifyIDTokenAndCheckRevoked(context.Background(), valid)
	require.NoError(t, err)
	require.Equal(t, uid, token.UID)
	for name, changes := range map[string]jwt.MapClaims{
		"wrong project": {"aud": "another-project"},
		"wrong issuer":  {"iss": "https://supabase.example/auth/v1"},
		"expired":       {"exp": now.Add(-time.Hour).Unix()},
		"future issued": {"iat": now.Add(time.Hour).Unix()},
		"empty subject": {"sub": ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := client.VerifyIDTokenAndCheckRevoked(context.Background(), sign(changes, key))
			require.Error(t, err)
		})
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	_, err = client.VerifyIDTokenAndCheckRevoked(context.Background(), sign(nil, otherKey))
	require.Error(t, err)
	disabled = true
	_, err = client.VerifyIDTokenAndCheckRevoked(context.Background(), valid)
	require.Error(t, err)
	disabled = false
	validSince = now.Unix()
	_, err = client.VerifyIDTokenAndCheckRevoked(context.Background(), valid)
	require.Error(t, err)
	deleted = true
	_, err = client.VerifyIDTokenAndCheckRevoked(context.Background(), valid)
	require.Error(t, err)
}
