package server

import (
	"os"
	"testing"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	glog "github.com/sirupsen/logrus"
)

func Test_Nodegroup(t *testing.T) {
	if os.Getenv("LOGLEVEL") == "DEBUG" {
		glog.SetLevel(glog.DebugLevel)
	}

	os.Setenv("TEST_MODE", "1")

	if utils.ShouldTestFeature("TestNodegroup") {
		test := createTestNodegroup(t, true)

		if utils.ShouldTestFeature("TestNodeGroup_launchVM") {
			t.Run("TestNodeGroup_launchVM", func(t *testing.T) {
				test.Child(t).launchVM()
			})
		}

		if utils.ShouldTestFeature("TestNodeGroup_stopVM") {
			t.Run("TestNodeGroup_stopVM", func(t *testing.T) {
				test.Child(t).stopVM()
			})
		}

		if utils.ShouldTestFeature("TestNodeGroup_startVM") {
			t.Run("TestNodeGroup_startVM", func(t *testing.T) {
				test.Child(t).startVM()
			})
		}

		if utils.ShouldTestFeature("TestNodeGroup_statusVM") {
			t.Run("TestNodeGroup_statusVM", func(t *testing.T) {
				test.Child(t).statusVM()
			})
		}

		if utils.ShouldTestFeature("TestNodeGroup_deleteVM") {
			t.Run("TestNodeGroup_deleteVM", func(t *testing.T) {
				test.Child(t).deleteVM()
			})
		}

		if utils.ShouldTestFeature("TestNodeGroupGroup_addNode") {
			t.Run("TestNodeGroupGroup_addNode", func(t *testing.T) {
				test.Child(t).addNode()
			})
		}

		if utils.ShouldTestFeature("TestNodeGroupGroup_deleteNode") {
			t.Run("TestNodeGroupGroup_deleteNode", func(t *testing.T) {
				test.Child(t).deleteNode()
			})
		}

		if utils.ShouldTestFeature("TestNodeGroupGroup_deleteNodeGroup") {
			t.Run("TestNodeGroupGroup_deleteNodeGroup", func(t *testing.T) {
				test.Child(t).deleteNodeGroup()
			})
		}
	}
}

func Test_Server(t *testing.T) {
	if utils.ShouldTestFeature("TestServer") {
		test := createServerTest(t, true)

		if utils.ShouldTestFeature("TestServer_NodeGroups") {
			t.Run("TestServer_NodeGroups", func(t *testing.T) {
				test.Child(t).NodeGroups()
			})
		}

		if utils.ShouldTestFeature("TestServer_NodeGroupForNode") {
			t.Run("TestServer_NodeGroupForNode", func(t *testing.T) {
				test.Child(t).NodeGroupForNode()
			})
		}

		if utils.ShouldTestFeature("TestServer_Pricing") {
			t.Run("TestServer_Pricing", func(t *testing.T) {
				test.Child(t).Pricing()
			})
		}

		if utils.ShouldTestFeature("TestServer_GetAvailableMachineTypes") {
			t.Run("TestServer_GetAvailableMachineTypes", func(t *testing.T) {
				test.Child(t).GetAvailableMachineTypes()
			})
		}

		if utils.ShouldTestFeature("TestServer_NewNodeGroup") {
			t.Run("TestServer_NewNodeGroup", func(t *testing.T) {
				test.Child(t).NewNodeGroup()
			})
		}

		if utils.ShouldTestFeature("TestServer_Create") {
			t.Run("TestServer_Create", func(t *testing.T) {
				test.Child(t).Create()
			})
		}

		if utils.ShouldTestFeature("TestServer_Delete") {
			t.Run("TestServer_Delete", func(t *testing.T) {
				test.Child(t).Delete()
			})
		}

		if utils.ShouldTestFeature("TestServer_GetResourceLimiter") {
			t.Run("TestServer_GetResourceLimiter", func(t *testing.T) {
				test.Child(t).GetResourceLimiter()
			})
		}

		if utils.ShouldTestFeature("TestServer_Refresh") {
			t.Run("TestServer_Refresh", func(t *testing.T) {
				test.Child(t).Refresh()
			})
		}

		if utils.ShouldTestFeature("TestServer_IncreaseSize") {
			t.Run("TestServer_IncreaseSize", func(t *testing.T) {
				test.Child(t).IncreaseSize()
			})
		}

		if utils.ShouldTestFeature("TestServer_DecreaseTargetSize") {
			t.Run("TestServer_DecreaseTargetSize", func(t *testing.T) {
				test.Child(t).DecreaseTargetSize()
			})
		}

		if utils.ShouldTestFeature("TestServer_TargetSize") {
			t.Run("TestServer_TargetSize", func(t *testing.T) {
				test.Child(t).TargetSize()
			})
		}

		if utils.ShouldTestFeature("TestServer_HasInstance") {
			t.Run("TestServer_HasInstance", func(t *testing.T) {
				test.Child(t).HasInstance()
			})
		}

		if utils.ShouldTestFeature("TestServer_Nodes") {
			t.Run("TestServer_Nodes", func(t *testing.T) {
				test.Child(t).Nodes()
			})
		}

		if utils.ShouldTestFeature("TestServer_DeleteNodes") {
			t.Run("TestServer_DeleteNodes", func(t *testing.T) {
				test.Child(t).DeleteNodes()
			})
		}

		if utils.ShouldTestFeature("TestServer_Id") {
			t.Run("TestServer_Id", func(t *testing.T) {
				test.Child(t).Id()
			})
		}

		if utils.ShouldTestFeature("TestServer_Debug") {
			t.Run("TestServer_Debug", func(t *testing.T) {
				test.Child(t).Debug()
			})
		}

		if utils.ShouldTestFeature("TestServer_TemplateNodeInfo") {
			t.Run("TestServer_TemplateNodeInfo", func(t *testing.T) {
				test.Child(t).TemplateNodeInfo()
			})
		}

		if utils.ShouldTestFeature("TestServer_Exist") {
			t.Run("TestServer_Exist", func(t *testing.T) {
				test.Child(t).Exist()
			})
		}

		if utils.ShouldTestFeature("TestServer_Autoprovisioned") {
			t.Run("TestServer_Autoprovisioned", func(t *testing.T) {
				test.Child(t).Autoprovisioned()
			})
		}

		if utils.ShouldTestFeature("TestServer_Belongs") {
			t.Run("TestServer_Belongs", func(t *testing.T) {
				test.Child(t).Belongs()
			})
		}

		if utils.ShouldTestFeature("TestServer_NodePrice") {
			t.Run("TestServer_NodePrice", func(t *testing.T) {
				test.Child(t).NodePrice()
			})
		}

		if utils.ShouldTestFeature("TestServer_PodPrice") {
			t.Run("TestServer_PodPrice", func(t *testing.T) {
				test.Child(t).PodPrice()
			})
		}

		if utils.ShouldTestFeature("TestServer_Cleanup") {
			t.Run("TestServer_Cleanup", func(t *testing.T) {
				test.Child(t).Cleanup()
			})
		}

	}
}
