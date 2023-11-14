package v1beta1

type SyncProviderType string

// comment duplicate
const (
	SyncProviderKubernetes SyncProviderType = "kubernetes"
	SyncProviderFilepath   SyncProviderType = "file"
	SyncProviderHttp       SyncProviderType = "http"
	SyncProviderGrpc       SyncProviderType = "grpc"
	SyncProviderFlagdProxy SyncProviderType = "flagd-proxy"
)
