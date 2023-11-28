// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
)

func TestUpgrade(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "upgrade test suites")
}

var (
	avalancheGoExecPath            string
	avalancheGoExecPathToUpgradeTo string
)

func init() {
	flag.StringVar(
		&avalancheGoExecPath,
		"avalanchego-path",
		"",
		"avalanchego executable path",
	)
	flag.StringVar(
		&avalancheGoExecPathToUpgradeTo,
		"avalanchego-path-to-upgrade-to",
		"",
		"avalanchego executable path to upgrade to",
	)
}

var _ = ginkgo.Describe("[Upgrade]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("can upgrade versions", func() {
		network := e2e.StartNetwork(avalancheGoExecPath, e2e.DefaultNetworkDir)

		ginkgo.By(fmt.Sprintf("restarting all nodes with %q binary", avalancheGoExecPathToUpgradeTo))
		for _, node := range network.Nodes {
			ginkgo.By(fmt.Sprintf("restarting node %q with %q binary", node.ID, avalancheGoExecPathToUpgradeTo))
			require.NoError(node.Stop(e2e.DefaultContext(), true /* waitForStopped */))

			node.DefaultRuntimeConfig.AvalancheGoPath = avalancheGoExecPathToUpgradeTo

			require.NoError(network.StartNode(e2e.DefaultContext(), ginkgo.GinkgoWriter, node))

			ginkgo.By(fmt.Sprintf("waiting for node %q to report healthy after restart", node.ID))
			e2e.WaitForHealthy(node)
		}

		e2e.CheckBootstrapIsPossible(network)
	})
})
