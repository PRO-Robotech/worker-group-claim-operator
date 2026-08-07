package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubeconfigKey is the Secret data key holding the kubeconfig.
const KubeconfigKey = "value"

const (
	requestTimeout = 10 * time.Second
	qps            = 20
	burst          = 40
)

type entry struct {
	client client.Client
	hash   string
}

// Cache hands out one non-caching client per target cluster, keyed by kubeconfig hash.
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
}

func NewCache() *Cache {
	return &Cache{entries: make(map[string]entry)}
}

// GetOrCreate returns a client for the kubeconfig stored under cacheKey.
func (c *Cache) GetOrCreate(cacheKey string, kubeconfig []byte) (client.Client, error) {
	sum := sha256.Sum256(kubeconfig)
	hash := hex.EncodeToString(sum[:])

	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[cacheKey]; ok && e.hash == hash {
		return e.client, nil
	}

	cl, err := build(kubeconfig)
	if err != nil {
		return nil, err
	}

	c.entries[cacheKey] = entry{client: cl, hash: hash}

	return cl, nil
}

// Forget drops the cached client.
func (c *Cache) Forget(cacheKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, cacheKey)
}

func build(kubeconfig []byte) (client.Client, error) {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	restCfg.Timeout = requestTimeout
	restCfg.QPS = qps
	restCfg.Burst = burst

	cl, err := client.New(restCfg, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}

	return cl, nil
}
