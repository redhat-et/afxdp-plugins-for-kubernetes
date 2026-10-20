module github.com/redhat-et/afxdp-plugins-for-kubernetes/pkg/goclient

go 1.25

require github.com/redhat-et/afxdp-plugins-for-kubernetes v0.0.0-00010101000000-000000000000

require (
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	golang.org/x/sys v0.24.0 // indirect
)

replace github.com/redhat-et/afxdp-plugins-for-kubernetes => ../..
