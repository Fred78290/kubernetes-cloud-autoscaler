package sshutils

import (
	"os/user"
	"strings"

	glog "github.com/sirupsen/logrus"
)

// AutoScalerServerSSH contains ssh client infos
type AutoScalerServerSSH struct {
	UserName              string `json:"user"`
	Password              string `json:"password"`
	AuthKeys              string `json:"ssh-private-key"`
	WaitSshReadyInSeconds int    `default:180 json:"wait-ssh-ready-seconds"`
	testMode              bool   `json:"-"`
}

func (ssh *AutoScalerServerSSH) SetMode(test bool) {
	ssh.testMode = test
}

func (ssh *AutoScalerServerSSH) GetMode() bool {
	return ssh.testMode
}

// GetUserName returns user name from config or the real current username is empty or equal to ~
func (ssh *AutoScalerServerSSH) GetUserName() string {
	if ssh.UserName == "" || ssh.UserName == "~" {
		u, err := user.Current()

		if err != nil {
			glog.Fatalf("Can't find current user! - %v", err)
		}

		return u.Username
	}

	return ssh.UserName
}

// GetAuthKeys returns the path to key file, subsistute ~
func (ssh *AutoScalerServerSSH) GetAuthKeys() string {
	if strings.Index(ssh.AuthKeys, "~") == 0 {
		u, err := user.Current()

		if err != nil {
			glog.Fatalf("Can't find current user! - %v", err)
		}

		return strings.Replace(ssh.AuthKeys, "~", u.HomeDir, 1)
	}

	return ssh.AuthKeys
}
