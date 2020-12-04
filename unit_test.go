package main

import (
	"github.com/jfrog/jfrog-cli-core/utils/coreutils"
	coreTests "github.com/jfrog/jfrog-cli-core/utils/tests"
	"github.com/jfrog/jfrog-cli/utils/tests"
	"github.com/jfrog/jfrog-client-go/utils/log"
	clientTests "github.com/jfrog/jfrog-client-go/utils/tests"
	"os"
	"path/filepath"
	"testing"
)

const (
	JfrogTestsHome      = ".jfrogTest"
	CliIntegrationTests = "github.com/jfrog/jfrog-cli"
)

func TestUnitTests(t *testing.T) {
	homePath, err := filepath.Abs(JfrogTestsHome)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}

	oldHome, err := coreTests.SetJfrogHome(homePath)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	defer os.Setenv(coreutils.HomeDir, oldHome)

	packages := clientTests.GetTestPackages("./...")
	packages = clientTests.ExcludeTestsPackage(packages, CliIntegrationTests)
	clientTests.RunTests(packages, *tests.HideUnitTestLog)
	coreTests.CleanUnitTestsJfrogHome(homePath)
}
