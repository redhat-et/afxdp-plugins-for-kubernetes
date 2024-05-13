module github.com/redhat-et/afxdp-plugins-for-kubernetes

go 1.19

require (
	github.com/containernetworking/cni v1.1.2
	github.com/containernetworking/plugins v1.1.1
	github.com/go-ozzo/ozzo-validation/v4 v4.3.0
	github.com/golang/protobuf v1.5.3
	github.com/google/gofuzz v1.1.0
	github.com/google/uuid v1.3.0
	github.com/moby/sys/mount v0.3.3
	github.com/pkg/errors v0.9.1
	github.com/redhat-et/afxdp-plugins-for-kubernetes/pkg/goclient v0.0.0
	github.com/redhat-et/afxdp-plugins-for-kubernetes/pkg/subfunctions v0.0.0
	github.com/safchain/ethtool v0.0.0-20210803160452-9aa261dae9b1
	github.com/sirupsen/logrus v1.9.0
	github.com/stretchr/testify v1.8.3
	github.com/vishvananda/netlink v1.1.1-0.20210330154013-f5de75959ad5
	golang.org/x/net v0.17.0
	google.golang.org/grpc v1.56.3
	gotest.tools v2.2.0+incompatible
	k8s.io/apimachinery v0.25.2
	k8s.io/kubelet v0.25.2
)

require (
	github.com/asaskevich/govalidator v0.0.0-20200108200545-475eaeb16496 // indirect
	github.com/coreos/go-iptables v0.6.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/vishvananda/netns v0.0.0-20210104183010-2eb08e3e575f // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/redhat-et/afxdp-plugins-for-kubernetes/pkg/subfunctions => ./pkg/subfunctions

replace github.com/redhat-et/afxdp-plugins-for-kubernetes/pkg/goclient => ./pkg/goclient
