package cni

// contains code for cnishim - one that gets called as the cni Plugin
// This does not do the real cni work. This is just the client to the cniserver
// that does the real work.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/Mellanox/sriovnet"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/config"
)

// Plugin is the structure to hold the endpoint information and the corresponding
// functions to use it
type Plugin struct {
	socketPath string
}

// NewCNIPlugin creates the internal Plugin object
func NewCNIPlugin(socketPath string) *Plugin {
	if len(socketPath) == 0 {
		socketPath = serverSocketPath
	}
	return &Plugin{socketPath: socketPath}
}

// Create and fill a Request with this Plugin's environment and stdin which
// contain the CNI variables and configuration
func newCNIRequest(args *skel.CmdArgs) *Request {
	envMap := make(map[string]string)
	for _, item := range os.Environ() {
		idx := strings.Index(item, "=")
		if idx > 0 {
			envMap[strings.TrimSpace(item[:idx])] = item[idx+1:]
		}
	}

	return &Request{
		Env:    envMap,
		Config: args.StdinData,
	}
}

// Send a CNI request to the CNI server via JSON + HTTP over a root-owned unix socket,
// and return the result
func (p *Plugin) doCNI(url string, req *Request) ([]byte, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CNI request %v: %v", req, err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(proto, addr string) (net.Conn, error) {
				var conn net.Conn
				if runtime.GOOS != "windows" {
					conn, err = net.Dial("unix", p.socketPath)
				} else {
					conn, err = net.Dial("tcp", serverTCPAddress)
				}
				return conn, err
			},
		},
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to send CNI request: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read CNI result: %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CNI request failed with status %v: '%s'", resp.StatusCode, string(body))
	}

	return body, nil
}

// CmdAdd is the callback for 'add' cni calls from skel
func (p *Plugin) CmdAdd(args *skel.CmdArgs) error {

	// read the config stdin args to obtain cniVersion
	conf, err := config.ReadCNIConfig(args.StdinData)
	if err != nil {
		return fmt.Errorf("invalid stdin args")
	}

	req := newCNIRequest(args)
        podReq, err := cniRequestToPodRequest(req)
        if err != nil {
		return fmt.Errorf("MOSHE!!! cniRequestToPodRequest")
        }
	// 1. set smart-nic pod annotation will add PF index and VF index
	// TODO:check for error
	SetPodInfoSmartNic(podReq.PodNamespace, podReq.PodName)
	if err != nil {
                 return err	
	}
        
	//body, err := p.doCNI("http://dummy/", req)
	//if err != nil {
	//	return err
	//}

	//response := &Response{}
	//if err = json.Unmarshal(body, response); err != nil {
	//	return fmt.Errorf("failed to unmarshal response '%s': %v", string(body), err)
        //}

	// 2. get POD annotation MAC/IP/GW/
	podInfo, _:= GetPodInfo(podReq.PodNamespace, podReq.PodName)
        podInterfaceInfo := &PodInterfaceInfo{
                PodAnnotation: *podInfo,
                MTU:           config.Default.MTU,
        }
        //1. get VF netdevice from PCI
        pciAddrs := "0000:81:00.3"
	vfNetdevices, err := sriovnet.GetNetDevicesFromPci(pciAddrs)
        if err != nil {
                return err

        }

        // Make sure we have 1 netdevice per pci address
        if len(vfNetdevices) != 1 {
                return fmt.Errorf("failed to get one netdevice interface per %s", pciAddrs)
        }
        vfNetdevice := vfNetdevices[0]	
	//3a. move to namespace VF
        netns, err := ns.GetNS(podReq.Netns) 
        err = moveIfToNetns(vfNetdevice, netns)
        if err != nil {
                return err
        }

        err = netns.Do(func(hostNS ns.NetNS) error {
                //contIface.Name = ifName
                err = renameLink(vfNetdevice, podReq.IfName)
                if err != nil {
                        return err
                }
                link, err := netlink.LinkByName(podReq.IfName)
                if err != nil {
                        return err
                }
                err = netlink.LinkSetMTU(link, podInterfaceInfo.MTU)
                if err != nil {
                        return err
                }
                err = netlink.LinkSetUp(link)
                if err != nil {
                        return err
                }

                err = setupNetwork(link, podInterfaceInfo)
                if err != nil {
                        return err
                }

                //contIface.Mac = ifInfo.MAC.String()
                //contIface.Sandbox = netns.Path()

                return nil
        })

	// 3.b setup network
	// set MTU


	var result *current.Result
	//if response.Result != nil {
	//	result = response.Result
	//} else {
	//	pr, _ := cniRequestToPodRequest(req)
	//	result = pr.getCNIResult(response.PodIFInfo)
	//	if result == nil {
	//		return fmt.Errorf("failed to get CNI Result from pod interface info %q", response.PodIFInfo)
	//	}
	//}
	//ipv4, _  := types.ParseCIDR(podInfo.IP)
	result = &current.Result{
		CNIVersion: "0.3.1",
		Interfaces: []*current.Interface{
			{
				Name:    "eth0",
				Mac:     podInfo.MAC.String(),
				Sandbox: podReq.Netns,
			},
		},
		IPs: []*current.IPConfig{
			{
				Version:   "4",
				Interface: current.Int(0),
				Address:   *podInfo.IP,
				Gateway:   net.ParseIP("192.168.0.1"),	
			},
		},
		Routes: []*types.Route{
		},
		DNS: types.DNS{
		},
	}
	return types.PrintResult(result, conf.CNIVersion)
}

// CmdDel is the callback for 'teardown' cni calls from skel
func (p *Plugin) CmdDel(args *skel.CmdArgs) error {
	// TODO need to release VF and fix it in standart ovs offload
	////_, err := p.doCNI("http://dummy/", newCNIRequest(args))
	return nil
}

// CmdCheck is the callback for 'checking' container's networking is as expected.
// Currently not implemented, so returns `nil`.
func (p *Plugin) CmdCheck(args *skel.CmdArgs) error {
	return nil
}
