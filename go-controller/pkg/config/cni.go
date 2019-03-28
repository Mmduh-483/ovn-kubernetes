package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"github.com/openvswitch/ovn-kubernetes/go-controller/pkg/types"
)

// WriteCNIConfig writes a CNI JSON config file to directory given by global config
func WriteCNIConfig() error {
	// These are promoted fileds in go and can't be assigned in the previous way
	netConf := &types.NetConf{}
	netConf.CNIVersion = "0.3.1"
	netConf.Name = "ovn-kubernetes"
	netConf.Type = CNI.Plugin
	bytes, err := json.Marshal(netConf)
	if err != nil {
		return fmt.Errorf("failed to marshal CNI config JSON: %v", err)
	}

	// Install the CNI config file after all initialization is done
	// MkdirAll() returns no error if the path already exists
	err = os.MkdirAll(CNI.ConfDir, os.ModeDir)
	if err != nil {
		return err
	}

	// Always create the CNI config for consistency.
	confFile := filepath.Join(CNI.ConfDir, "10-ovn-kubernetes.conf")

	var f *os.File
	f, err = ioutil.TempFile(CNI.ConfDir, "ovnkube-")
	if err != nil {
		return err
	}

	_, err = f.Write(bytes)
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	return os.Rename(f.Name(), confFile)
}

// ReadCNIConfig unmarshals a CNI JSON config into an NetConf structure
func ReadCNIConfig(bytes []byte) (*types.NetConf, error) {
	conf := &types.NetConf{}
	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, err
	}
	return conf, nil
}
