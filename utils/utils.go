package utils

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"time"

	glog "github.com/sirupsen/logrus"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/drone/envsubst"
	"gopkg.in/yaml.v2"
	apiv1 "k8s.io/api/core/v1"
)

func GetTestMode() bool {
	if testMode := strings.ToLower(os.Getenv("TEST_MODE")); testMode != "" {
		return testMode == "true" || testMode == "1"
	}

	return false
}

func GetServerConfigFile() (config string) {
	if config = os.Getenv("TEST_SERVER_CONFIG"); config == "" {
		glog.Fatal("TEST_SERVER_CONFIG not defined")
	}

	return
}

func GetConfigFile() (config string) {
	if config = os.Getenv("TEST_CONFIG"); config == "" {
		glog.Fatal("TEST_CONFIG not defined")
	}

	return
}

func GetMachinesConfigFile() (config string) {
	if config = os.Getenv("TEST_MACHINES_CONFIG"); config == "" {
		glog.Fatal("TEST_MACHINES_CONFIG not defined")
	}

	return
}

func GetProviderConfigFile() (config string) {
	if config = os.Getenv("TEST_PROVIDER_CONFIG"); config == "" {
		glog.Fatal("TEST_PROVIDER_CONFIG not defined")
	}

	return
}

func LoadTextEnvSubst(fileName string) (string, error) {
	if buf, err := os.ReadFile(fileName); err != nil {
		return "", err
	} else {
		return envsubst.EvalEnv(string(buf))
	}
}

func LoadConfig(fileName string, config any) (err error) {
	var content string

	if content, err = LoadTextEnvSubst(fileName); err == nil {
		err = json.NewDecoder(strings.NewReader(content)).Decode(config)

		if glog.GetLevel() > glog.DebugLevel {
			glog.Debugf("loaded: %s", fileName)
			glog.Debug(ToYAML(config))
		}
	}

	return
}

// ShouldTestFeature check if test must be done
func ShouldTestFeature(name string) bool {
	if feature := os.Getenv(name); feature != "" {
		return feature != "NO"
	}

	return true
}

// NodeFromJSON deserialize a string to apiv1.Node
func NodeFromJSON(s string) (*apiv1.Node, error) {
	data := &apiv1.Node{}

	err := json.Unmarshal([]byte(s), &data)

	return data, err
}

// ToYAML serialize interface to yaml
func ToYAML(v any) string {
	if v == nil {
		return ""
	}

	b, _ := yaml.Marshal(v)

	return string(b)
}

// ToJSON serialize interface to json
func ToJSON(v any) string {
	if v == nil {
		return ""
	}

	b, _ := json.Marshal(v)

	return string(b)
}

func DirExistAndReadable(name string) bool {
	if len(name) == 0 {
		return false
	}

	if entry, err := os.Stat(name); err != nil {
		return false
	} else if entry.IsDir() {

		if files, err := os.ReadDir(name); err == nil {
			for _, file := range files {
				if entry, err := file.Info(); err == nil {
					if !entry.IsDir() {
						fm := entry.Mode()
						sys := entry.Sys().(*syscall.Stat_t)

						if (fm&(1<<2) != 0) || ((fm&(1<<5)) != 0 && os.Getegid() == int(sys.Gid)) || ((fm&(1<<8)) != 0 && (os.Geteuid() == int(sys.Uid))) {
							continue
						}
					} else if DirExistAndReadable(name + "/" + entry.Name()) {
						continue
					}
				}

				return false
			}

			return true
		}
	}

	return false
}

func FileExistAndReadable(name string) bool {
	if len(name) == 0 {
		return false
	}

	if entry, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	} else {
		fm := entry.Mode()
		sys := entry.Sys().(*syscall.Stat_t)

		if (fm&(1<<2) != 0) || ((fm&(1<<5)) != 0 && os.Getegid() == int(sys.Gid)) || ((fm&(1<<8)) != 0 && (os.Geteuid() == int(sys.Uid))) {
			return true
		}
	}

	return false
}

// FileExists Check if FileExists
func FileExists(name string) bool {
	if len(name) == 0 {
		return false
	}

	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}

	return true
}

// MinInt min(a,b)
func MinInt(x, y int) int {
	if x < y {
		return x
	}
	return y
}

// MaxInt max(a,b)
func MaxInt(x, y int) int {
	if x > y {
		return x
	}
	return y
}

// MinInt64 min(a,b)
func MinInt64(x, y int64) int64 {
	if x < y {
		return x
	}
	return y
}

// MaxInt64 max(a,b)
func MaxInt64(x, y int64) int64 {
	if x > y {
		return x
	}
	return y
}

func NewRequestContext(requestTimeout time.Duration) *context.Context {
	return context.NewContext(time.Duration(requestTimeout.Seconds()))
}

// Values returns the values of the map m.
// The values will be in an indeterminate order.
func Values[M ~map[K]V, K comparable, V any](m M) []V {
	r := make([]V, 0, len(m))
	for _, v := range m {
		r = append(r, v)
	}
	return r
}
