package types

import (
	"github.com/containernetworking/cni/pkg/types"
)

type NetConf  struct {
	types.NetConf
	// PciAddrs in case of using sriov
	DeviceID string `json:"deviceID"`
}
