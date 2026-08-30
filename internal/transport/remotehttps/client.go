package remotehttps

import (
	"fmt"
	"strings"

	workerclient "agentbus/internal/client/worker"
)

func NewWorkerClient(baseURL string, tlsConfig Config) (*workerclient.UnixHTTPWorkerClient, error) {
	if !strings.HasPrefix(baseURL, "https://") {
		return nil, fmt.Errorf("remote Worker endpoint must use https")
	}
	httpClient, err := NewHTTPClient(tlsConfig)
	if err != nil {
		return nil, err
	}
	return workerclient.NewHTTPWorkerClient(baseURL, httpClient)
}
