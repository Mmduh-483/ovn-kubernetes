// +build linux

package cni

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"io/ioutil"
	"strconv"

	"github.com/sirupsen/logrus"

	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

func renameLink(curName, newName string) error {
	link, err := netlink.LinkByName(curName)
	if err != nil {
		return err
	}

	if err := netlink.LinkSetDown(link); err != nil {
		return err
	}
	if err := netlink.LinkSetName(link, newName); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}

	return nil
}

func setupInterface(netns ns.NetNS, containerID, ifName, macAddress, ipAddress, gatewayIP string, mtu int) (*current.Interface, *current.Interface, error) {
	hostIface := &current.Interface{}
	contIface := &current.Interface{}

	var oldHostVethName string
	err := netns.Do(func(hostNS ns.NetNS) error {
		// create the veth pair in the container and move host end into host netns
		hostVeth, containerVeth, err := ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}
		hostIface.Mac = hostVeth.HardwareAddr.String()
		contIface.Name = containerVeth.Name

		link, err := netlink.LinkByName(contIface.Name)
		if err != nil {
			return fmt.Errorf("failed to lookup %s: %v", contIface.Name, err)
		}

		hwAddr, err := net.ParseMAC(macAddress)
		if err != nil {
			return fmt.Errorf("failed to parse mac address for %s: %v", contIface.Name, err)
		}
		err = netlink.LinkSetHardwareAddr(link, hwAddr)
		if err != nil {
			return fmt.Errorf("failed to add mac address %s to %s: %v", macAddress, contIface.Name, err)
		}
		contIface.Mac = macAddress
		contIface.Sandbox = netns.Path()

		addr, err := netlink.ParseAddr(ipAddress)
		if err != nil {
			return err
		}
		err = netlink.AddrAdd(link, addr)
		if err != nil {
			return fmt.Errorf("failed to add IP addr %s to %s: %v", ipAddress, contIface.Name, err)
		}

		gw := net.ParseIP(gatewayIP)
		if gw == nil {
			return fmt.Errorf("parse ip of gateway failed")
		}
		err = ip.AddRoute(nil, gw, link)
		if err != nil {
			return err
		}

		oldHostVethName = hostVeth.Name

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// rename the host end of veth pair
	hostIface.Name = containerID[:15]
	if err := renameLink(oldHostVethName, hostIface.Name); err != nil {
		return nil, nil, fmt.Errorf("failed to rename %s to %s: %v", oldHostVethName, hostIface.Name, err)
	}

	return hostIface, contIface, nil
}

// Setup sriov interface in the pod
func setupSriovInterface(netns ns.NetNS, containerID, ifName, macAddress, ipAddress, gatewayIP string, mtu int, pciAddrs string) (*current.Interface, *current.Interface, error) {
	hostIface := &current.Interface{}
	contIface := &current.Interface{}

	vfName, err := GetVfNameFromPciAddrs(pciAddrs)
	vfRepName, err := GetVfRepresentorName(vfName)
	if err != nil {
		return nil, nil, err
	}
	contIface.Name = ifName
	hostIface.Name = vfRepName

	// getting VF representor
	link, err := netlink.LinkByName(hostIface.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %s: %v", hostIface.Name, err)
	}

	hostIface.Mac = link.Attrs().HardwareAddr.String()

	// moving the contIface to the namespace and getting its Hardware Address
	link, err = netlink.LinkByName(vfName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %s: %v", contIface.Name, err)
        }
	hwAddr, err := net.ParseMAC(macAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse mac address for %s: %v", contIface.Name, err)
	}
	err = netlink.LinkSetHardwareAddr(link, hwAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to add mac address %s to %s: %v", macAddress, contIface.Name, err)
	}
	// Rename the VF with  a temp name to avoid conflict
	err = netlink.LinkSetName(link, "tmp123")
	if err != nil {
		return nil, nil, err
	}
	// move VF device to ns
	if err = netlink.LinkSetNsFd(link, int(netns.Fd())); err != nil {
		return nil, nil, fmt.Errorf("failed to move device %+v to netns: %q", contIface.Name, err)
	}
	err = netns.Do(func(hostNS ns.NetNS) error {
		// Change VF name back to the wanted name of CNI_IFNAME
	        link, err = netlink.LinkByName("tmp123")
		if err != nil {
			return fmt.Errorf("failed to lookup %s: %v", contIface.Name, err)
	        }
		err = netlink.LinkSetName(link, ifName)
		if err != nil {
			return err
		}
		if err := netlink.LinkSetUp(link); err != nil {
		        return err
		}


		contIface.Mac = macAddress
		contIface.Sandbox = netns.Path()

		addr, err := netlink.ParseAddr(ipAddress)
		if err != nil {
			return err
		}
		err = netlink.AddrAdd(link, addr)
		if err != nil {
			return fmt.Errorf("failed to add IP addr %s to %s: %v", ipAddress, contIface.Name, err)
		}

		gw := net.ParseIP(gatewayIP)
		if gw == nil {
			return fmt.Errorf("parse ip of gateway failed")
		}
		err = ip.AddRoute(nil, gw, link)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// rename the VF representor
	if err := renameLink(hostIface.Name, containerID[:15]); err != nil {
		return nil, nil, fmt.Errorf("failed to rename %s to %s: %v", hostIface.Name,containerID[:15], err)
	}
	hostIface.Name = containerID[:15]

	return hostIface, contIface, nil
}

// ConfigureInterface sets up the container interface
func (pr *PodRequest) ConfigureInterface(namespace string, podName string, macAddress string, ipAddress string, gatewayIP string, mtu int, ingress, egress int64) ([]*current.Interface, error) {
	netns, err := ns.GetNS(pr.Netns)
	if err != nil {
		return nil, fmt.Errorf("failed to open netns %q: %v", pr.Netns, err)
	}
	defer netns.Close()

	var hostIface, contIface *current.Interface

	if pr.CNIConf.DeviceID != "" {
		// Sriov Case
		hostIface, contIface, err = setupSriovInterface(netns, pr.SandboxID, pr.IfName, macAddress, ipAddress, gatewayIP, mtu, pr.CNIConf.DeviceID)
		if err != nil {
			return nil, err
		}
	} else {
		// OvnKube general case
		hostIface, contIface, err = setupInterface(netns, pr.SandboxID, pr.IfName, macAddress, ipAddress, gatewayIP, mtu)
		if err != nil {
			return nil, err
		}
	}

	ifaceID := fmt.Sprintf("%s_%s", namespace, podName)

	ovsArgs := []string{
		"add-port", "br-int", hostIface.Name, "--", "set",
		"interface", hostIface.Name,
		fmt.Sprintf("external_ids:attached_mac=%s", macAddress),
		fmt.Sprintf("external_ids:iface-id=%s", ifaceID),
		fmt.Sprintf("external_ids:ip_address=%s", ipAddress),
		fmt.Sprintf("external_ids:sandbox=%s", pr.SandboxID),
	}

	if out, err := ovsExec(ovsArgs...); err != nil {
		return nil, fmt.Errorf("failure in plugging pod interface: %v\n  %q", err, out)
	}

	if err := clearPodBandwidth(pr.SandboxID); err != nil {
		return nil, err
	}

	if ingress > 0 || egress > 0 {
		l, err := netlink.LinkByName(hostIface.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to find host veth interface %s: %v", hostIface.Name, err)
		}
		err = netlink.LinkSetTxQLen(l, 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to set host veth txqlen: %v", err)
		}

		if err := setPodBandwidth(pr.SandboxID, hostIface.Name, ingress, egress); err != nil {
			return nil, err
		}
	}

	return []*current.Interface{hostIface, contIface}, nil
}

// PlatformSpecificCleanup deletes the OVS port
func (pr *PodRequest) PlatformSpecificCleanup() error {
	ifaceName := pr.SandboxID[:15]
	ovsArgs := []string{
		"del-port", "br-int", ifaceName,
	}
	out, err := exec.Command("ovs-vsctl", ovsArgs...).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "no port named") {
		// DEL should be idempotent; don't return an error just log it
		logrus.Warningf("failed to delete OVS port %s: %v\n  %q", ifaceName, err, string(out))
	}

	_ = clearPodBandwidth(pr.SandboxID)

	return nil
}

func GetVfNameFromPciAddrs (vfPciAddrs string) (string, error) {
	pciDir := fmt.Sprintf("/sys/bus/pci/devices/%s/net", vfPciAddrs)
	info, err := ioutil.ReadDir(pciDir)
	if err != nil {
		return "", err
	}
	return info[0].Name(), nil
}

func GetVfRepresentorName(vfName string) (string, error) {
	vfIndex, err := GetVfIndex(vfName)
	if err != nil {
		return "", err
	}
	logrus.Errorf("VF index %v", vfIndex)
	pfName, err := GetPfNameFromVfName(vfName)
	if err != nil {
		return "", err
	}
	pfSwitchDevId, err := GetNetDevSwitchDevId(pfName)
	if err != nil {
		return "", err
	}
	info, err := ioutil.ReadDir("/sys/class/net")
	for _, netDev := range info {
		netDevSwitchId, err := GetNetDevSwitchDevId(netDev.Name())
		if err != nil {
			continue
		}
		portIndex, err := GetNetDevPortIndex(netDev.Name())
		if err != nil {
			continue
		}
		if portIndex == vfIndex && netDevSwitchId == pfSwitchDevId {
			return netDev.Name(), nil
		}
	}
	return "", fmt.Errorf("Could not found VF Representor for VF %v", vfName)
}

func GetPfNameFromVfName(vfName string) (string, error) {
	netDevDir := fmt.Sprintf("/sys/class/net/%s/device/physfn/net", vfName)
	info, err := ioutil.ReadDir(netDevDir)
	if err != nil {
		return "", err
	}
	return info[0].Name(), nil
}

func GetVfIndex (vfName string) (int, error){
	var vf int
	pfName, err := GetPfNameFromVfName(vfName)
	if err != nil {
		return vf, err
	}
	vfTotal, err := GetPfTotalVfsNumber(pfName)
	if err != nil {
		return vf, err
	}
	for vf = 0; vf <= (vfTotal - 1); vf++ {
		vfDir := fmt.Sprintf("/sys/class/net/%s/device/virtfn%d/net", pfName, vf)
		infos, err := ioutil.ReadDir(vfDir)
		if err != nil {
			return vf, fmt.Errorf("failed to read the virtfn%d dir of the device %q: %v", vf, pfName, err)
		}
		if len(infos) == 0 {
			continue
		}
		if infos[0].Name() == vfName {
			return vf, nil
		}
	}
	return vf, fmt.Errorf("Failed to get %v index", vfName)
}

func GetPfTotalVfsNumber(pfName string) (int, error) {
	var vfTotal int
	sriovFile := fmt.Sprintf("/sys/class/net/%s/device/sriov_numvfs", pfName)
	data, err := ioutil.ReadFile(sriovFile)
	if err != nil {
		return vfTotal, fmt.Errorf("failed to read the sriov_numfs of device %q: %v", pfName, err)
	}
	sriovNumfs := strings.TrimSpace(string(data))
	i64, err := strconv.ParseInt(sriovNumfs, 10, 0)
	if err != nil {
		return vfTotal, fmt.Errorf("failed to convert sriov_numfs(byte value) to int of device %q: %v", pfName, err)
	}
	return int(i64), nil
}

func GetNetDevSwitchDevId (netDevName string) (string, error){
	switchDevIdFile := fmt.Sprintf("/sys/class/net/%s/phys_switch_id", netDevName)
	data, err := ioutil.ReadFile(switchDevIdFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func GetNetDevPortIndex (netDevName string) (int, error){
	var portIndex int
	portIdFile := fmt.Sprintf("/sys/class/net/%s/phys_port_name", netDevName)
	data, err := ioutil.ReadFile(portIdFile)
	if err != nil {
		return portIndex, err
	}
	strData := strings.TrimSpace(string(data))
	i64, err := strconv.ParseInt(strData, 10, 0)
	if err != nil {
		return portIndex, fmt.Errorf("failed to convert Net Dev %v Index, %v", netDevName, err)
	}
	portIndex = int(i64)
	return portIndex, nil
}
