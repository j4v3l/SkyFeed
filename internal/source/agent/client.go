package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const credentialFileName = "credentials.json"
const agentUserAgent = "SkyFeed-Agent/dev (github.com/j4v3l/SkyFeed)"

type Credentials struct {
	FeederID   domain.FeederID    `json:"feeder_id"`
	PrivateKey ed25519.PrivateKey `json:"private_key"`
	Sequence   uint64             `json:"sequence"`
}

type Client struct {
	base       *url.URL
	httpClient *http.Client
	stateDir   string
	now        func() time.Time
	mu         sync.Mutex
	credential Credentials
	pending    *SignedEnvelope
}

func NewClient(serverURL, stateDir string, allowPrivateHTTP bool) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("agent server URL must be an absolute URL without credentials, query, or fragment")
	}
	if base.Scheme != "https" {
		if !allowPrivateHTTP || base.Scheme != "http" || !privateHost(base.Hostname()) {
			return nil, errors.New("agent server URL must use HTTPS; private HTTP requires an explicit override")
		}
	}
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("agent state directory must be absolute")
	}
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 2,
		IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
	}
	return &Client{
		base: base, stateDir: stateDir, now: time.Now,
		httpClient: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func privateHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	// RFC 6598 shared address space is commonly used by private mesh VPNs.
	value := ip.To4()
	return value != nil && value[0] == 100 && value[1] >= 64 && value[1] <= 127
}

func (client *Client) Enroll(ctx context.Context, token string) (Credentials, error) {
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 256 {
		return Credentials{}, errors.New("enrollment token is invalid")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Credentials{}, fmt.Errorf("generate agent identity: %w", err)
	}
	requestBody, _ := json.Marshal(enrollmentRequest{Token: token, PublicKey: publicKey})
	var response enrollmentResponse
	if err := client.post(ctx, "/v1/agent/enroll", requestBody, &response); err != nil {
		return Credentials{}, err
	}
	if response.Version != ProtocolVersion || !response.FeederID.Valid() || response.FeederID == domain.FeederAll {
		return Credentials{}, errors.New("enrollment response is invalid")
	}
	credential := Credentials{FeederID: response.FeederID, PrivateKey: privateKey}
	if err := client.saveCredentials(credential); err != nil {
		return Credentials{}, err
	}
	client.mu.Lock()
	client.credential = credential
	client.mu.Unlock()
	return credential, nil
}

func (client *Client) LoadCredentials() (Credentials, error) {
	data, err := os.ReadFile(filepath.Join(client.stateDir, credentialFileName))
	if err != nil {
		return Credentials{}, fmt.Errorf("read agent credentials: %w", err)
	}
	if len(data) > 8<<10 {
		return Credentials{}, errors.New("agent credential file is oversized")
	}
	var credential Credentials
	if err := json.Unmarshal(data, &credential); err != nil {
		return Credentials{}, fmt.Errorf("decode agent credentials: %w", err)
	}
	if !credential.FeederID.Valid() || credential.FeederID == domain.FeederAll || len(credential.PrivateKey) != ed25519.PrivateKeySize {
		return Credentials{}, errors.New("agent credential file is invalid")
	}
	client.mu.Lock()
	client.credential = credential
	client.mu.Unlock()
	return credential, nil
}

func (client *Client) Send(ctx context.Context, snapshot *domain.Snapshot) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.credential.PrivateKey) != ed25519.PrivateKeySize {
		return errors.New("agent is not enrolled")
	}
	if client.pending == nil {
		payload, err := EncodeSnapshot(snapshot)
		if err != nil {
			return err
		}
		nextSequence := client.credential.Sequence + 1
		envelope, err := SignEnvelope(client.credential.PrivateKey, client.credential.FeederID, nextSequence, client.now().UTC(), payload)
		if err != nil {
			return err
		}
		client.credential.Sequence = nextSequence
		if err := client.saveCredentials(client.credential); err != nil {
			return err
		}
		client.pending = &envelope
	}
	requestBody, err := json.Marshal(client.pending)
	if err != nil {
		return fmt.Errorf("encode snapshot envelope: %w", err)
	}
	var response struct {
		Accepted bool `json:"accepted"`
	}
	if err := client.post(ctx, "/v1/agent/snapshots", requestBody, &response); err != nil {
		return err
	}
	if !response.Accepted {
		return errors.New("snapshot was not accepted")
	}
	client.pending = nil
	return nil
}

func (client *Client) post(ctx context.Context, path string, body []byte, destination any) error {
	endpoint := *client.base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", agentUserAgent)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("agent request failed: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 64<<10)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, limited)
		return fmt.Errorf("agent server returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(limited).Decode(destination); err != nil {
		return fmt.Errorf("decode agent response: %w", err)
	}
	return nil
}

func (client *Client) saveCredentials(credential Credentials) error {
	if err := os.MkdirAll(client.stateDir, 0o700); err != nil {
		return fmt.Errorf("create agent state directory: %w", err)
	}
	data, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode agent credentials: %w", err)
	}
	temporary, err := os.CreateTemp(client.stateDir, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create agent credential file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if chmodErr := temporary.Chmod(0o600); chmodErr != nil {
		err = chmodErr
	} else {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write agent credentials: %w", err)
	}
	if err := os.Rename(temporaryName, filepath.Join(client.stateDir, credentialFileName)); err != nil {
		return fmt.Errorf("install agent credentials: %w", err)
	}
	return nil
}
