package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/go-connections/nat"
	"github.com/spikeekips/mitum/base/key"
	"gopkg.in/yaml.v3"

	"github.com/spikeekips/contest/config"
)

var DefaultContainerCmdNodeInit = []string{
	"/runner", "node", "init",
	"--log-level", "debug",
	"--log-format", "json",
	"/config.yml",
}

var DefaultContainerCmdNodeRun = []string{
	"/runner", "node", "run",
	"--log-level", "debug",
	"--log-format", "json",
	"/config.yml",
}

type Node struct {
	sync.RWMutex
	alias        string
	host         Host
	design       string
	commonDesign string
	portMap      nat.PortMap
	keyMap       map[string]key.Privatekey
	templateLock sync.RWMutex
	configData   []byte
	configMap    map[string]interface{}
}

func NewNode(alias string, host Host) (*Node, error) {
	no := &Node{
		alias:   alias,
		host:    host,
		portMap: nat.PortMap{},
		keyMap:  map[string]key.Privatekey{},
	}

	return no, nil
}

func (no *Node) Alias() string {
	return no.alias
}

func (no *Node) Host() Host {
	return no.host
}

func (no *Node) ConfigData() []byte {
	return no.configData
}

func (no *Node) ConfigFile() string {
	return filepath.Join(no.host.BaseDir(), fmt.Sprintf("%s.yml", no.alias))
}

func (no *Node) ConfigMap() map[string]interface{} {
	return no.configMap
}

func (no *Node) PortMap() nat.PortMap {
	return no.portMap
}

func (no *Node) Prepare(commonDesign, design string, vars *config.Vars) (map[string]interface{}, error) {
	no.Lock()
	defer no.Unlock()

	no.commonDesign = commonDesign
	no.design = design

	nvars := no.prepareVars(vars)

	var shared map[string]interface{}
	if i, err := no.prepareNodeConfig(nvars); err != nil {
		return nil, err
	} else {
		shared = i
	}

	return shared, nil
}

func (no *Node) prepareNodeConfig(vars *config.Vars) (map[string]interface{}, error) {
	var merged map[string]interface{}
	if nc, err := parseTemplateConfig(no.design, vars); err != nil {
		return nil, err
	} else if cc, err := parseTemplateConfig(no.commonDesign, vars); err != nil {
		return nil, err
	} else if m, err := config.MergeItem(cc, nc); err != nil {
		return nil, err
	} else {
		merged = m
	}

	filtered := map[string]interface{}{}
	shared := map[string]interface{}{}
	for k := range merged {
		if !strings.HasPrefix(k, "_") {
			filtered[k] = merged[k]

			continue
		}

		shared[k[1:]] = merged[k]
	}

	if b, err := yaml.Marshal(filtered); err != nil {
		return nil, err
	} else {
		no.configMap = config.SanitizeVarsMap(filtered).(map[string]interface{})
		no.configData = bytes.TrimSpace(b)
	}

	return shared, nil
}

func (no *Node) prepareVars(vars *config.Vars) *config.Vars {
	self := map[string]interface{}{
		"Alias": no.alias,
		"Host":  no.host.Host(),
	}

	vars.Set("Self", self)

	_ = vars.AddFunc("ContainerBindPort", no.containerBindPort)

	return vars
}

func (no *Node) containerBindPort(name, network, sourcePort string) string {
	no.templateLock.Lock()
	defer no.templateLock.Unlock()

	if port, err := no.host.AvailablePort(name, network); err != nil {
		panic(err)
	} else {
		var found bool

	end:
		for _, ps := range no.portMap {
			for i := range ps {
				if ps[i].HostPort == port {
					found = true

					break end
				}
			}
		}
		if !found {
			if source, err := nat.NewPort(network, sourcePort); err != nil {
				panic(err)
			} else {
				no.portMap[source] = []nat.PortBinding{
					{HostIP: "", HostPort: port},
				}
			}
		}

		return port
	}
}

func parseTemplateConfig(s string, vars *config.Vars) (map[string]interface{}, error) {
	var p []byte
	if i, err := config.CompileTemplate(s, vars); err != nil {
		return nil, err
	} else {
		p = i
	}

	var m map[string]interface{}
	if err := yaml.Unmarshal(p, &m); err != nil {
		return nil, err
	}

	return m, nil
}

type NodeExistedMsg struct {
	StatusCode int64  `json:"status_code"`
	Msg        string `json:"m"`
	Err        error  `json:"error"`
}

func (msg NodeExistedMsg) MarshalJSON() ([]byte, error) {
	var err string
	if msg.Err != nil {
		err = fmt.Sprintf("%+v", msg.Err)
	}

	return json.Marshal(map[string]interface{}{
		"status_code": msg.StatusCode,
		"m":           msg.Msg,
		"error":       err,
	})
}
