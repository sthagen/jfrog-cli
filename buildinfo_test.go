package main

import (
	"encoding/json"
	"fmt"
	"github.com/jfrog/jfrog-client-go/utils/log"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jfrog/jfrog-cli-core/artifactory/commands/generic"
	"github.com/jfrog/jfrog-cli-core/artifactory/spec"
	"github.com/jfrog/jfrog-cli-core/artifactory/utils"
	"github.com/jfrog/jfrog-cli-core/utils/coreutils"
	coretests "github.com/jfrog/jfrog-cli-core/utils/tests"
	"github.com/jfrog/jfrog-cli/inttestutils"
	"github.com/jfrog/jfrog-cli/utils/cliutils"
	"github.com/jfrog/jfrog-cli/utils/tests"
	"github.com/jfrog/jfrog-client-go/artifactory/buildinfo"
	"github.com/jfrog/jfrog-client-go/artifactory/services"
	rtutils "github.com/jfrog/jfrog-client-go/artifactory/services/utils"
	clientutils "github.com/jfrog/jfrog-client-go/utils"
	"github.com/jfrog/jfrog-client-go/utils/io/fileutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ModuleNameJFrogTest = "jfrog-test"

func TestBuildAddDependenciesFromHomeDir(t *testing.T) {
	initArtifactoryTest(t)
	// Clean old build tests if exists
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)

	fileName := "cliTestFile.txt"
	testFileRelPath, testFileAbs := createFileInHomeDir(t, fileName)

	test := buildAddDepsBuildInfoTestParams{description: "'rt bad' from home dir", commandArgs: []string{testFileRelPath, "--recursive=false"}, expectedDependencies: []string{fileName}, buildName: tests.RtBuildName1, buildNumber: "1"}
	collectDepsAndPublishBuild(test, false, t)
	validateBuildAddDepsBuildInfo(t, test)

	os.Remove(testFileAbs)
	cleanArtifactoryTest()
}

func TestBuildPromote(t *testing.T) {
	initArtifactoryTest(t)
	buildNumberA := "10"

	// Upload files with buildName and buildNumber
	specFileA, err := tests.CreateSpec(tests.SplitUploadSpecA)
	assert.NoError(t, err)
	artifactoryCli.Exec("upload", "--spec="+specFileA, "--build-name="+tests.RtBuildName1, "--build-number="+buildNumberA)
	artifactoryCli.Exec("build-publish", tests.RtBuildName1, buildNumberA)

	key1 := "key"
	value1 := "v1,v2"
	key2 := "another"
	value2 := "property"

	// Promote build to Repo1 using build name and build number as args.
	assert.NoError(t, artifactoryCli.Exec("build-promote", tests.RtBuildName1, buildNumberA, tests.RtRepo1, fmt.Sprintf("--props=%s=%s;%s=%s", key1, value1, key2, value2)))
	publishedBuildInfo, found, err := tests.GetBuildInfo(serverDetails, tests.RtBuildName1, buildNumberA)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	if !found {
		assert.True(t, found, "build info was expected to be found")
		return
	}
	buildInfo := publishedBuildInfo.BuildInfo
	resultItems := getResultItemsFromArtifactory(tests.SearchAllRepo1, t)

	assert.Equal(t, len(buildInfo.Modules[0].Artifacts), len(resultItems), "Incorrect number of artifacts were uploaded")

	// Promote the same build to Repo2 using build name and build number as env vars.
	assert.NoError(t, os.Setenv(coreutils.BuildName, tests.RtBuildName1))
	assert.NoError(t, os.Setenv(coreutils.BuildNumber, buildNumberA))
	defer os.Unsetenv(coreutils.BuildName)
	defer os.Unsetenv(coreutils.BuildNumber)
	assert.NoError(t, artifactoryCli.Exec("build-promote", tests.RtRepo2, fmt.Sprintf("--props=%s=%s;%s=%s", key1, value1, key2, value2)))

	publishedBuildInfo, found, err = tests.GetBuildInfo(serverDetails, tests.RtBuildName1, buildNumberA)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	if !found {
		assert.True(t, found, "build info was expected to be found")
		return
	}
	buildInfo = publishedBuildInfo.BuildInfo

	resultItems = getResultItemsFromArtifactory(tests.SearchRepo2, t)

	assert.Equal(t, len(buildInfo.Modules[0].Artifacts), len(resultItems), "Incorrect number of artifacts were uploaded")

	propsMap := map[string][]string{
		"build.name":   {buildInfo.Name},
		"build.number": {buildInfo.Number},
		key1:           strings.Split(value1, ","),
		key2:           {value2},
	}

	validateArtifactsProperties(resultItems, t, propsMap)
	cleanArtifactoryTest()
}

// Returns the artifacts found by the provided spec
func getResultItemsFromArtifactory(specName string, t *testing.T) []rtutils.ResultItem {
	searchGoSpecFile, err := tests.CreateSpec(specName)
	assert.NoError(t, err)
	spec, flags := getSpecAndCommonFlags(searchGoSpecFile)
	var resultItems []rtutils.ResultItem
	for i := 0; i < len(spec.Files); i++ {
		searchParams, err := utils.GetSearchParams(spec.Get(i))
		assert.NoError(t, err)
		reader, err := services.SearchBySpecFiles(searchParams, flags, rtutils.ALL)
		assert.NoError(t, err, "Failed Searching files")
		for searchResult := new(rtutils.ResultItem); reader.NextRecord(searchResult) == nil; searchResult = new(rtutils.ResultItem) {
			resultItems = append(resultItems, *searchResult)
		}
		assert.NoError(t, reader.GetError())
		assert.NoError(t, reader.Close())
	}
	return resultItems
}

// This function validates the properties on the provided artifacts. Every property within the provided map should be attached to the artifact.
func validateArtifactsProperties(resultItems []rtutils.ResultItem, t *testing.T, propsMap map[string][]string) {
	for _, item := range resultItems {
		properties := item.Properties
		assert.GreaterOrEqual(t, len(properties), 1, "Failed finding properties on item:", item.GetItemRelativePath())
		propertiesMap := tests.ConvertSliceToMap(properties)

		for key, values := range propsMap {
			valuesFromArtifact, contains := propertiesMap[key]
			assert.True(t, contains, "Failed finding %s property on %s", key, item.Name)
			for _, value := range values {
				assert.Contains(t, valuesFromArtifact, value, "Wrong value for %s property on %s", key, item.Name)
			}
		}
	}
}

func TestBuildAddDependenciesDryRun(t *testing.T) {
	initArtifactoryTest(t)
	// Clean old build tests if exists
	err := utils.RemoveBuildDir(tests.RtBuildName1, "1", "")
	assert.NoError(t, err)

	wd, err := os.Getwd()
	assert.NoError(t, err)

	defer os.Chdir(wd)
	err = os.Chdir("testdata")
	assert.NoError(t, err)

	noCredsCli := tests.NewJfrogCli(execMain, "jfrog rt", "")
	// Execute the bad command on the local file system
	noCredsCli.Exec("bad", tests.RtBuildName1, "1", "a/*", "--dry-run=true")
	buildDir, err := utils.GetBuildDir(tests.RtBuildName1, "1", "")
	assert.NoError(t, err)

	files, _ := ioutil.ReadDir(buildDir)
	assert.Zero(t, len(files), "'rt bad' command with dry-run failed. The dry-run option has no effect.")

	// Execute the bad command on remote Artifactory
	artifactoryCli.Exec("upload", "a/*", tests.RtRepo1)
	noCredsCli.Exec("bad", tests.RtBuildName1, "2", tests.RtRepo1+"/*", "--from-rt", "--dry-run=true")
	buildDir, err = utils.GetBuildDir(tests.RtBuildName1, "2", "")
	assert.NoError(t, err)

	files, _ = ioutil.ReadDir(buildDir)
	assert.Zero(t, len(files), "'rt bad' command on remote with dry-run failed. The dry-run option has no effect.")

	os.Chdir(wd)
	cleanArtifactoryTest()
}

func TestBuildPublishDetailedSummary(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber := "11"

	// Clean old build tests if exists.
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	assert.NoError(t, utils.RemoveBuildDir(tests.RtBuildName1, buildNumber, ""))

	// Upload files with build name & number.
	specFile, err := tests.CreateSpec(tests.UploadFlatNonRecursive)
	assert.NoError(t, err)
	assert.NoError(t, artifactoryCli.Exec("upload", "--spec="+specFile, "--build-name="+tests.RtBuildName1, "--build-number="+buildNumber))
	// Verify build dir is not empty
	assert.NotEmpty(t, getFilesFromBuildDir(t, tests.RtBuildName1, buildNumber, ""))

	buffer, previousLog := tests.RedirectLogOutputToBuffer()
	// Restore previous logger when the function returns
	defer log.SetLogger(previousLog)
	// Execute the bp command with --detailed-summary.
	assert.NoError(t, artifactoryCli.Exec("bp", tests.RtBuildName1, buildNumber, "--detailed-summary=true"))
	tests.VerifySha256DetailedSummaryFromBuffer(t, buffer, previousLog)

	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	cleanArtifactoryTest()
}

func TestBuildPublishDryRun(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber := "11"
	// Clean old build tests if exists.
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	assert.NoError(t, utils.RemoveBuildDir(tests.RtBuildName1, buildNumber, ""))

	// Upload files with build name & number.
	specFile, err := tests.CreateSpec(tests.UploadFlatRecursive)
	assert.NoError(t, err)
	assert.NoError(t, artifactoryCli.Exec("upload", "--spec="+specFile, "--build-name="+tests.RtBuildName1, "--build-number="+buildNumber))
	// Verify build dir is not empty
	assert.NotEmpty(t, getFilesFromBuildDir(t, tests.RtBuildName1, buildNumber, ""))

	// Execute the bp command with dry run.
	assert.NoError(t, artifactoryCli.Exec("bp", tests.RtBuildName1, buildNumber, "--dry-run=true"))
	// Verify build dir is not empty.
	assert.NotEmpty(t, getFilesFromBuildDir(t, tests.RtBuildName1, buildNumber, ""))
	// Verify build was not published.
	_, found, err := tests.GetBuildInfo(serverDetails, tests.RtBuildName1, buildNumber)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	if found {
		assert.False(t, found, "build info was expected not to be found")
		return
	}

	// Execute the bp command without dry run
	assert.NoError(t, artifactoryCli.Exec("bp", tests.RtBuildName1, buildNumber))
	// Verify build dir is empty
	assert.Empty(t, getFilesFromBuildDir(t, tests.RtBuildName1, buildNumber, ""))
	// Verify build was published
	publishedBuildInfo, found, err := tests.GetBuildInfo(serverDetails, tests.RtBuildName1, buildNumber)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	if !found {
		assert.True(t, found, "build info was expected to be found")
		return
	}
	buildInfo := publishedBuildInfo.BuildInfo
	validateBuildInfo(buildInfo, t, 0, 9, tests.RtBuildName1, buildinfo.Generic)

	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	cleanArtifactoryTest()
}

func getFilesFromBuildDir(t *testing.T, buildName, buildNumber, projectKey string) []os.FileInfo {
	buildDir, err := utils.GetBuildDir(buildName, buildNumber, "")
	assert.NoError(t, err)

	files, err := ioutil.ReadDir(buildDir)
	assert.NoError(t, err)
	return files
}

func TestBuildAppend(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber1 := "12"
	buildNumber2 := "13"

	// Clean old builds tests if exists
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName2, artHttpDetails)

	// Publish build RtBuildName1/buildNumber1
	err := artifactoryCli.WithoutCredentials().Exec("bce", tests.RtBuildName1, buildNumber1)
	assert.NoError(t, err)
	err = artifactoryCli.Exec("bp", tests.RtBuildName1, buildNumber1)
	assert.NoError(t, err)

	// Create a build RtBuildName2/buildNumber2 and append RtBuildName1/buildNumber1 to the build
	err = artifactoryCli.Exec("ba", tests.RtBuildName2, buildNumber2, tests.RtBuildName1, buildNumber1)
	assert.NoError(t, err)

	// Assert RtBuildName2/buildNumber2 is appended to RtBuildName1/buildNumber1 locally
	partials, err := utils.ReadPartialBuildInfoFiles(tests.RtBuildName2, buildNumber2, "")
	assert.NoError(t, err)
	assert.Len(t, partials, 1)
	assert.Equal(t, tests.RtBuildName1+"/"+buildNumber1, partials[0].ModuleId)
	assert.Equal(t, buildinfo.Build, partials[0].ModuleType)
	assert.NotZero(t, partials[0].Timestamp)
	assert.NotNil(t, partials[0].Checksum)
	assert.NotZero(t, partials[0].Checksum.Md5)
	assert.NotZero(t, partials[0].Checksum.Sha1)

	// Publish build RtBuildName2/buildNumber2
	err = artifactoryCli.Exec("bp", tests.RtBuildName2, buildNumber2)
	assert.NoError(t, err)

	// Check published build info
	buildInfo, _, err := tests.GetBuildInfo(serverDetails, tests.RtBuildName2, buildNumber2)
	assert.NoError(t, err)
	assert.NotNil(t, buildInfo)
	assert.Len(t, buildInfo.BuildInfo.Modules, 1)
	module := buildInfo.BuildInfo.Modules[0]
	assert.Equal(t, tests.RtBuildName1+"/"+buildNumber1, module.Id)
	assert.Equal(t, buildinfo.Build, module.Type)

	// Clean builds
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName2, artHttpDetails)
	cleanArtifactoryTest()
}

func TestDownloadAppendedBuild(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber1 := "12"
	buildNumber2 := "13"

	// Clean old builds tests if exists
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName2, artHttpDetails)

	// Add files to RtBuildName1/buildNumber1.
	specFile, err := tests.CreateSpec(tests.SplitUploadSpecA)
	assert.NoError(t, err)
	assert.NoError(t, artifactoryCli.Exec("upload", "--spec="+specFile, "--build-name="+tests.RtBuildName1, "--build-number="+buildNumber1))

	// Publish build RtBuildName1/buildNumber1
	err = artifactoryCli.Exec("bp", tests.RtBuildName1, buildNumber1)
	assert.NoError(t, err)

	// Create a build RtBuildName2/buildNumber2 and append RtBuildName1/buildNumber1 to the build
	err = artifactoryCli.Exec("ba", tests.RtBuildName2, buildNumber2, tests.RtBuildName1, buildNumber1)
	assert.NoError(t, err)

	// Publish build RtBuildName2/buildNumber2
	err = artifactoryCli.Exec("bp", tests.RtBuildName2, buildNumber2)
	assert.NoError(t, err)

	// Download
	err = artifactoryCli.Exec("dl", tests.RtRepo1, filepath.Join(tests.Out, "download", "simple_by_build")+fileutils.GetFileSeparator(), "--build="+tests.RtBuildName2+"/"+buildNumber2)
	assert.NoError(t, err)

	// Validate files from
	paths, _ := fileutils.ListFilesRecursiveWalkIntoDirSymlink(tests.Out, false)
	err = tests.ValidateListsIdentical(tests.GetBuildSimpleDownloadNoPattern(), paths)
	assert.NoError(t, err)

	// Clean builds
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName2, artHttpDetails)
	cleanArtifactoryTest()
}

func TestSearchAppendedBuildNoPattern(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber1 := "12"
	buildNumber2 := "13"

	// Clean old builds tests if exists
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName2, artHttpDetails)

	// Add files to RtBuildName1/buildNumber1.
	specFile, err := tests.CreateSpec(tests.SplitUploadSpecA)
	assert.NoError(t, err)
	assert.NoError(t, artifactoryCli.Exec("upload", "--spec="+specFile, "--build-name="+tests.RtBuildName1, "--build-number="+buildNumber1))

	// Publish build RtBuildName1/buildNumber1
	err = artifactoryCli.Exec("bp", tests.RtBuildName1, buildNumber1)
	assert.NoError(t, err)

	// Create a build RtBuildName2/buildNumber2 and append RtBuildName1/buildNumber1 to the build
	err = artifactoryCli.Exec("ba", tests.RtBuildName2, buildNumber2, tests.RtBuildName1, buildNumber1)
	assert.NoError(t, err)

	// Publish build RtBuildName2/buildNumber2
	err = artifactoryCli.Exec("bp", tests.RtBuildName2, buildNumber2)
	assert.NoError(t, err)

	// Run search on RtBuildName2/buildNumber2
	searchCmd := generic.NewSearchCommand()
	searchCmd.SetServerDetails(serverDetails)
	searchCmd.SetSpec(spec.NewBuilder().Build(tests.RtBuildName2 + "/" + buildNumber2).BuildSpec())
	reader, err := searchCmd.Search()
	assert.NoError(t, err)

	// Make sure artifacts from RtBuildName1/buildNumber1 are matched by searching on RtBuildName2/buildNumber2
	length, err := reader.Length()
	assert.NoError(t, err)
	assert.Equal(t, 3, length)
	for resultItem := new(utils.SearchResult); reader.NextRecord(resultItem) == nil; resultItem = new(utils.SearchResult) {
		assert.Contains(t, tests.GetSearchAppendedBuildNoPatternExpected(), resultItem.Path)
		assert.Equal(t, []string{tests.RtBuildName1}, resultItem.Props["build.name"])
		assert.Equal(t, []string{buildNumber1}, resultItem.Props["build.number"])
	}

	// Clean builds
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName2, artHttpDetails)
	cleanArtifactoryTest()
}

func TestBuildAddDependencies(t *testing.T) {
	initArtifactoryTest(t)
	createJfrogHomeConfig(t, true)
	// Clean old build tests if exists
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	buildAddDepsRemoteSpec, err := tests.CreateSpec(tests.BuildAddDepsRemoteSpec)
	assert.NoError(t, err)
	buildAddDepsDoubleRemoteSpec, err := tests.CreateSpec(tests.BuildAddDepsDoubleRemoteSpec)
	assert.NoError(t, err)
	artifactoryCli.Exec("upload", "testdata/a/*", tests.RtRepo1, "--flat=false")
	allFiles := []string{"a1.in", "a2.in", "a3.in", "b1.in", "b2.in", "b3.in", "c1.in", "c2.in", "c3.in"}
	var badTests = []buildAddDepsBuildInfoTestParams{
		// Collect the dependencies from the local file system (the --from-rt option is not used).
		{description: "'rt bad' simple cli", commandArgs: []string{"testdata/a/*"}, expectedDependencies: allFiles},
		{description: "'rt bad' single file", commandArgs: []string{"testdata/a/a1.in"}, expectedDependencies: []string{"a1.in"}},
		{description: "'rt bad' none recursive", commandArgs: []string{"testdata/a/*", "--recursive=false"}, expectedDependencies: []string{"a1.in", "a2.in", "a3.in"}},
		{description: "'rt bad' special chars recursive", commandArgs: []string{getSpecialCharFilePath()}, expectedDependencies: []string{"a1.in"}},
		{description: "'rt bad' exclude command line wildcards", commandArgs: []string{"testdata/a/*", "--exclusions=*a2*;*a3.in"}, expectedDependencies: []string{"a1.in", "b1.in", "b2.in", "b3.in", "c1.in", "c2.in", "c3.in"}},
		{description: "'rt bad' spec", commandArgs: []string{"--spec=" + tests.GetFilePathForArtifactory(tests.BuildAddDepsSpec)}, expectedDependencies: allFiles},
		{description: "'rt bad' two specFiles", commandArgs: []string{"--spec=" + tests.GetFilePathForArtifactory(tests.BuildAddDepsDoubleSpec)}, expectedDependencies: []string{"a1.in", "a2.in", "a3.in", "b1.in", "b2.in", "b3.in"}},
		{description: "'rt bad' exclude command line regexp", commandArgs: []string{"testdata/a/a(.*)", "--exclusions=(.*)a2.*;.*a3.in", "--regexp=true"}, expectedDependencies: []string{"a1.in"}},

		// Collect the dependencies from Artifactory using the --from-rt option.
		{description: "'rt bad' simple cli", commandArgs: []string{tests.RtRepo1 + "/testdata/a/*", "--from-rt"}, expectedDependencies: allFiles},
		{description: "'rt bad' single file", commandArgs: []string{tests.RtRepo1 + "/testdata/a/a1.in", "--from-rt"}, expectedDependencies: []string{"a1.in"}},
		{description: "'rt bad' none recursive", commandArgs: []string{tests.RtRepo1 + "/testdata/a/*", "--recursive=false", "--from-rt"}, expectedDependencies: []string{"a1.in", "a2.in", "a3.in"}},
		{description: "'rt bad' exclude command line wildcards", commandArgs: []string{tests.RtRepo1 + "/testdata/a/*", "--exclusions=*a2*;*a3.in", "--from-rt"}, expectedDependencies: []string{"a1.in", "b1.in", "b2.in", "b3.in", "c1.in", "c2.in", "c3.in"}},
		{description: "'rt bad' spec", commandArgs: []string{"--spec=" + buildAddDepsRemoteSpec, "--from-rt"}, expectedDependencies: allFiles},
		{description: "'rt bad' two specFiles", commandArgs: []string{"--spec=" + buildAddDepsDoubleRemoteSpec, "--from-rt"}, expectedDependencies: []string{"a1.in", "a2.in", "a3.in", "b1.in", "b2.in", "b3.in"}},
	}

	// Tests compatibility to file paths with windows separators.
	if coreutils.IsWindows() {
		var compatibilityTests = []buildAddDepsBuildInfoTestParams{
			{description: "'rt bad' win compatibility by arguments", commandArgs: []string{"testdata\\\\a\\\\a1.in"}, expectedDependencies: []string{"a1.in"}},
			{description: "'rt bad' win compatibility by spec", commandArgs: []string{"--spec=" + tests.GetFilePathForArtifactory(tests.WinBuildAddDepsSpec)}, expectedDependencies: allFiles},
		}
		badTests = append(badTests, compatibilityTests...)
	}

	for i, badTest := range badTests {
		badTest.buildName = tests.RtBuildName1
		badTest.buildNumber = strconv.Itoa(i + 1)

		collectDepsAndPublishBuild(badTest, true, t)
		validateBuildAddDepsBuildInfo(t, badTest)
		utils.RemoveBuildDir(badTest.buildName, badTest.buildNumber, "")

		collectDepsAndPublishBuild(badTest, false, t)
		validateBuildAddDepsBuildInfo(t, badTest)
		utils.RemoveBuildDir(badTest.buildName, badTest.buildNumber, "")
	}
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	cleanArtifactoryTest()
}

// Test publish build info without --build-url
func TestArtifactoryPublishAndGetBuildInfo(t *testing.T) {
	testArtifactoryPublishWithoutBuildUrl(t, tests.RtBuildName1, "10")
}

// Test publish and get build info with spaces in name.
func TestArtifactoryPublishAndGetBuildInfoSpecialChars(t *testing.T) {
	testArtifactoryPublishWithoutBuildUrl(t, tests.RtBuildNameWithSpecialChars, "11")
}

func testArtifactoryPublishWithoutBuildUrl(t *testing.T, buildName, buildNumber string) {
	initArtifactoryTest(t)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, buildName, artHttpDetails)

	bi, err := uploadFilesAndGetBuildInfo(t, buildName, buildNumber, "")
	if err != nil {
		return
	}

	assert.Equal(t, buildName, bi.Name)
	assert.NotEmpty(t, bi.Modules)
	// Validate no build url.
	assert.Empty(t, bi.BuildUrl)

	// Cleanup
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, buildName, artHttpDetails)
	cleanArtifactoryTest()
}

// Test publish build info with --build-url
func TestArtifactoryPublishBuildInfoBuildUrl(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber := "11"
	buildUrl := "http://example.ci.com"
	os.Setenv(cliutils.BuildUrl, "http://override-me.ci.com")
	defer os.Unsetenv(cliutils.BuildUrl)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)

	bi, err := uploadFilesAndGetBuildInfo(t, tests.RtBuildName1, buildNumber, buildUrl)
	if err != nil {
		return
	}
	// Validate correctness of build url
	assert.Equal(t, buildUrl, bi.BuildUrl)

	// Cleanup
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	cleanArtifactoryTest()
}

// Test publish build info with JFROG_CLI_BUILD_URL env
func TestArtifactoryPublishBuildInfoBuildUrlFromEnv(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber := "11"
	buildUrl := "http://example-env.ci.com"
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	os.Setenv(cliutils.BuildUrl, buildUrl)
	defer os.Unsetenv(cliutils.BuildUrl)

	bi, err := uploadFilesAndGetBuildInfo(t, tests.RtBuildName1, buildNumber, "")
	if err != nil {
		return
	}

	// Validate correctness of build url.
	assert.Equal(t, buildUrl, bi.BuildUrl)

	// Cleanup
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	cleanArtifactoryTest()
}

func TestGetNonExistingBuildInfo(t *testing.T) {
	initArtifactoryTest(t)
	buildName := "jfrog-cli-rt-tests-non-existing-build-info"
	buildNumber := "10"
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, buildName, artHttpDetails)

	_, found, err := tests.GetBuildInfo(serverDetails, buildName, buildNumber)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	assert.False(t, found, "build info was expected not to be found")

	// Cleanup
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, buildName, artHttpDetails)
	cleanArtifactoryTest()
}

func TestArtifactoryCleanBuildInfo(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber := "11"
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)

	// Upload files with buildName and buildNumber
	specFile, err := tests.CreateSpec(tests.SplitUploadSpecA)
	assert.NoError(t, err)
	artifactoryCli.Exec("upload", "--spec="+specFile, "--build-name="+tests.RtBuildName1, "--build-number="+buildNumber)

	// Cleanup buildInfo with the same buildName and buildNumber
	artifactoryCli.WithoutCredentials().Exec("build-clean", tests.RtBuildName1, buildNumber)

	// Upload different files with the same buildName and buildNumber
	specFile, err = tests.CreateSpec(tests.SplitUploadSpecB)
	assert.NoError(t, err)
	artifactoryCli.Exec("upload", "--spec="+specFile, "--build-name="+tests.RtBuildName1, "--build-number="+buildNumber)

	// Publish buildInfo
	artifactoryCli.Exec("build-publish", tests.RtBuildName1, buildNumber)

	// Download by build and verify that only artifacts uploaded after clean are downloaded
	outputDir := filepath.Join(tests.Out, "clean-build")
	artifactoryCli.Exec("download", tests.RtRepo1, outputDir+fileutils.GetFileSeparator(), "--build="+tests.RtBuildName1+"/"+buildNumber)
	paths, _ := fileutils.ListFilesRecursiveWalkIntoDirSymlink(outputDir, false)
	tests.VerifyExistLocally(tests.GetCleanBuild(), paths, t)

	// Cleanup
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	cleanArtifactoryTest()
}

func TestArtifactoryBuildCollectEnv(t *testing.T) {
	initArtifactoryTest(t)
	buildNumber := "12"

	// Build collect env
	assert.NoError(t, os.Setenv("DONT_COLLECT", "foo"))
	assert.NoError(t, os.Setenv("COLLECT", "bar"))
	assert.NoError(t, artifactoryCli.WithoutCredentials().Exec("bce", tests.RtBuildName1, buildNumber))

	// Publish build info
	assert.NoError(t, artifactoryCli.Exec("bp", tests.RtBuildName1, buildNumber, "--env-exclude=*password*;*psw*;*secret*;*key*;*token*;DONT_COLLECT"))
	publishedBuildInfo, found, err := tests.GetBuildInfo(serverDetails, tests.RtBuildName1, buildNumber)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	if !found {
		assert.True(t, found, "build info was expected to be found")
		return
	}
	buildInfo := publishedBuildInfo.BuildInfo

	// Make sure no sensitive data in build env
	for k := range buildInfo.Properties {
		assert.NotContains(t, k, "password")
		assert.NotContains(t, k, "psw")
		assert.NotContains(t, k, "secret")
		assert.NotContains(t, k, "key")
		assert.NotContains(t, k, "token")
		assert.NotContains(t, k, "DONT_COLLECT")
	}

	// Make sure "COLLECT" env appear in build env
	assert.Contains(t, buildInfo.Properties, "buildInfo.env.COLLECT")

	// Cleanup
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	cleanArtifactoryTest()
}

func TestBuildAddGit(t *testing.T) {
	testBuildAddGit(t, false)
}

func TestBuildAddGitEnvBuildNameAndNumber(t *testing.T) {
	testBuildAddGit(t, true)
}

func testBuildAddGit(t *testing.T, useEnvBuildNameAndNumber bool) {
	initArtifactoryTest(t)
	gitCollectCliRunner := tests.NewJfrogCli(execMain, "jfrog rt", "")
	buildNumber := "13"

	// Populate cli config with 'default' server
	oldHomeDir := os.Getenv(coreutils.HomeDir)
	createJfrogHomeConfig(t, true)

	// Create .git folder for this test
	originalFolder := "buildaddgit_.git_suffix"
	baseDir, dotGitPath := coretests.PrepareDotGitDir(t, originalFolder, "testdata")
	defer cleanBuildAddGitTest(t, baseDir, originalFolder, oldHomeDir, dotGitPath)

	// Get path for build-add-git config file
	pwd, _ := os.Getwd()
	configPath := filepath.Join(pwd, "testdata", "buildaddgit_config.yaml")

	// Run build-add-git
	var err error
	if useEnvBuildNameAndNumber {
		assert.NoError(t, os.Setenv(coreutils.BuildName, tests.RtBuildName1))
		assert.NoError(t, os.Setenv(coreutils.BuildNumber, buildNumber))
		defer os.Unsetenv(coreutils.BuildName)
		defer os.Unsetenv(coreutils.BuildNumber)
		err = gitCollectCliRunner.Exec("build-add-git", baseDir, "--config="+configPath)
	} else {
		err = gitCollectCliRunner.Exec("build-add-git", tests.RtBuildName1, buildNumber, baseDir, "--config="+configPath)
	}
	if err != nil {
		t.Fatal(err)
	}

	// Check partials VCS info
	partials, err := utils.ReadPartialBuildInfoFiles(tests.RtBuildName1, buildNumber, "")
	assert.NoError(t, err)
	expectedVcsUrl := "https://github.com/jfrog/jfrog-cli-go.git"
	expectedVcsRevision := "b033a0e508bdb52eee25654c9e12db33ff01b8ff"
	expectedVcsBranch := "master"
	expectedVcsMessage := "TEST-4 - Adding text to file2.txt"
	buildInfoVcsUrl := partials[0].VcsList[0].Url
	buildInfoVcsRevision := partials[0].VcsList[0].Revision
	buildInfoVcsBranch := partials[0].VcsList[0].Branch
	buildInfoVcsMessage := partials[0].VcsList[0].Message
	assert.Equal(t, expectedVcsUrl, buildInfoVcsUrl, "Wrong url")
	assert.Equal(t, expectedVcsRevision, buildInfoVcsRevision, "Wrong revision")
	assert.Equal(t, expectedVcsBranch, buildInfoVcsBranch, "Wrong branch")
	assert.Equal(t, expectedVcsMessage, buildInfoVcsMessage, "Wrong Message")

	// Clear previous build if exists and publish build-info.
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	assert.NoError(t, artifactoryCli.Exec("build-publish", tests.RtBuildName1, buildNumber))

	// Fetch the published build-info for validation.
	publishedBuildInfo, found, err := tests.GetBuildInfo(serverDetails, tests.RtBuildName1, buildNumber)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	if !found {
		assert.True(t, found, "build info was expected to be found")
		return
	}
	buildInfo := publishedBuildInfo.BuildInfo
	require.NotNil(t, buildInfo.VcsList, "Received build-info with empty VCS.")

	// Validate results
	buildInfoVcsUrl = buildInfo.VcsList[0].Url
	buildInfoVcsRevision = buildInfo.VcsList[0].Revision
	assert.Equal(t, expectedVcsRevision, buildInfoVcsRevision, "Wrong revision")
	assert.Equal(t, expectedVcsUrl, buildInfoVcsUrl, "Wrong url")
	assert.False(t, buildInfo.Issues == nil || len(buildInfo.Issues.AffectedIssues) != 4,
		"Wrong issues number, expected 4 issues, received: %+v", *buildInfo.Issues)
	cleanArtifactoryTest()
}

func cleanBuildAddGitTest(t *testing.T, baseDir, originalFolder, oldHomeDir, dotGitPath string) {
	coretests.RenamePath(dotGitPath, filepath.Join(baseDir, originalFolder), t)
	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, tests.RtBuildName1, artHttpDetails)
	os.Setenv(coreutils.HomeDir, oldHomeDir)
	cleanArtifactoryTest()
}

func TestReadGitConfig(t *testing.T) {
	dotGitPath := getCliDotGitPath(t)
	gitManager := clientutils.NewGitManager(dotGitPath)
	err := gitManager.ReadConfig()
	assert.NoError(t, err, "Failed to read .git config file.")

	workingDir, err := os.Getwd()
	assert.NoError(t, err, "Failed to get current dir.")
	gitExecutor := tests.GitExecutor(workingDir)
	revision, _, err := gitExecutor.GetRevision()
	assert.NoError(t, err)
	assert.Equal(t, revision, gitManager.GetRevision(), "Wrong revision")

	url, _, err := gitExecutor.GetUrl()
	assert.NoError(t, err)
	if !strings.HasSuffix(url, ".git") {
		url += ".git"
	}
	assert.Equal(t, url, gitManager.GetUrl(), "Wrong url")

	branch, _, err := gitExecutor.GetBranch()
	assert.NoError(t, err)
	assert.Equal(t, branch, gitManager.GetBranch(), "Wrong branch")

	message, _, err := gitExecutor.GetMessage(revision)
	assert.NoError(t, err)
	assert.Equal(t, message, gitManager.GetMessage(), "Wrong message")

	cleanArtifactoryTest()
}

func uploadFilesAndGetBuildInfo(t *testing.T, buildName, buildNumber, buildUrl string) (buildinfo.BuildInfo, error) {
	uploadFiles(t, "upload", "--build-name="+buildName, "--build-number="+buildNumber)

	// Publish buildInfo.
	publishBuildInfoArgs := []string{"build-publish", buildName, buildNumber}
	if buildUrl != "" {
		publishBuildInfoArgs = append(publishBuildInfoArgs, "--build-url="+buildUrl)
	}
	err := artifactoryCli.Exec(publishBuildInfoArgs...)
	if err != nil {
		assert.NoError(t, err)
		return buildinfo.BuildInfo{}, err
	}

	// Validate files are uploaded with the build info name and number.
	props := fmt.Sprintf("build.name=%v;build.number=%v", buildName, buildNumber)
	verifyExistInArtifactoryByProps(tests.GetSimpleUploadExpectedRepo1(), tests.RtRepo1+"/*", props, t)

	// Get build info.
	publishedBuildInfo, found, err := tests.GetBuildInfo(serverDetails, buildName, buildNumber)
	if err != nil {
		assert.NoError(t, err)
		return buildinfo.BuildInfo{}, err
	}
	if !found {
		assert.True(t, found, "build info was expected to be found")
		return buildinfo.BuildInfo{}, err
	}
	buildInfo := publishedBuildInfo.BuildInfo
	validateBuildInfo(buildInfo, t, 0, 9, buildName, buildinfo.Generic)
	return buildInfo, nil
}

func uploadFiles(t *testing.T, args ...string) {
	// Upload files with buildName and buildNumber
	specFile, err := tests.CreateSpec(tests.UploadFlatRecursive)
	assert.NoError(t, err)
	args = append(args, "--spec="+specFile)
	artifactoryCli.Exec(args...)
}

func downloadFiles(t *testing.T, args ...string) {
	// Download files with buildName and buildNumber
	specFile, err := tests.CreateSpec(tests.DownloadAllRepo1TestResources)
	assert.NoError(t, err)
	args = append(args, "--spec="+specFile)
	artifactoryCli.Exec(args...)
}

func TestModuleName(t *testing.T) {
	initArtifactoryTest(t)
	buildName := tests.RtBuildName1
	type command struct {
		execFunc func(t *testing.T, args ...string)
		args     []string
	}

	testsArray := []struct {
		testName             string
		buildNumber          string
		moduleName           string
		expectedDependencies int
		expectedArtifacts    int
		execCommands         []command
	}{
		{"uploadWithModuleChange", "9", ModuleNameJFrogTest, 0, 9, []command{{uploadFiles, []string{"upload", "--build-name=" + buildName, "--module=" + ModuleNameJFrogTest}}}},
		{"uploadWithoutChange", "10", buildName, 0, 9, []command{{uploadFiles, []string{"upload", "--build-name=" + buildName}}}},
		{"downloadWithModuleChange", "11", ModuleNameJFrogTest, 9, 0, []command{{downloadFiles, []string{"download", "--build-name=" + buildName, "--module=" + ModuleNameJFrogTest}}}},
		{"downloadWithoutModuleChange", "12", buildName, 9, 0, []command{{downloadFiles, []string{"download", "--build-name=" + buildName}}}},
		{"uploadAndDownloadAggregationWithModuleChange", "13", ModuleNameJFrogTest, 9, 9, []command{{uploadFiles, []string{"upload", "--build-name=" + buildName, "--module=" + ModuleNameJFrogTest}}, {downloadFiles, []string{"download", "--build-name=" + buildName, "--module=" + ModuleNameJFrogTest}}}},
		{"uploadAndDownloadAggregationWithoutModuleChange", "14", buildName, 9, 9, []command{{uploadFiles, []string{"upload", "--build-name=" + buildName}}, {downloadFiles, []string{"download", "--build-name=" + buildName}}}},
	}

	for _, test := range testsArray {
		t.Run(test.testName, func(t *testing.T) {
			for _, exeCommand := range test.execCommands {
				exeCommand.args = append(exeCommand.args, "--build-number="+test.buildNumber)
				exeCommand.execFunc(t, exeCommand.args...)
			}
			assert.NoError(t, artifactoryCli.Exec("bp", buildName, test.buildNumber))
			publishedBuildInfo, found, err := tests.GetBuildInfo(serverDetails, buildName, test.buildNumber)
			if err != nil {
				assert.NoError(t, err)
				return
			}
			if !found {
				assert.True(t, found, "build info was expected to be found")
				return
			}
			buildInfo := publishedBuildInfo.BuildInfo
			validateBuildInfo(buildInfo, t, test.expectedDependencies, test.expectedArtifacts, test.moduleName, buildinfo.Generic)
		})
	}

	inttestutils.DeleteBuild(serverDetails.ArtifactoryUrl, buildName, artHttpDetails)
	cleanArtifactoryTest()
}

func collectDepsAndPublishBuild(badTest buildAddDepsBuildInfoTestParams, useEnvBuildNameAndNumber bool, t *testing.T) {
	noCredsCli := tests.NewJfrogCli(execMain, "jfrog rt", "")
	// Remove old tests data from fs if exists
	err := utils.RemoveBuildDir(badTest.buildName, badTest.buildNumber, "")
	assert.NoError(t, err)

	command := []string{"bad"}
	if useEnvBuildNameAndNumber {
		os.Setenv(coreutils.BuildName, badTest.buildName)
		os.Setenv(coreutils.BuildNumber, badTest.buildNumber)
		defer os.Unsetenv(coreutils.BuildName)
		defer os.Unsetenv(coreutils.BuildNumber)
	} else {
		command = append(command, badTest.buildName, badTest.buildNumber)
	}

	// Execute tha bad command
	assert.NoError(t, noCredsCli.Exec(append(command, badTest.commandArgs...)...))
	assert.NoError(t, artifactoryCli.Exec("bp", badTest.buildName, badTest.buildNumber))
}

func validateBuildAddDepsBuildInfo(t *testing.T, buildInfoTestParams buildAddDepsBuildInfoTestParams) {
	publishedBuildInfo, found, err := tests.GetBuildInfo(serverDetails, buildInfoTestParams.buildName, buildInfoTestParams.buildNumber)
	if err != nil {
		assert.NoError(t, err)
		return
	}
	if !found {
		assert.True(t, found, "build info was expected to be found")
		return
	}
	buildInfo := publishedBuildInfo.BuildInfo
	if buildInfo.Modules == nil || len(buildInfo.Modules) == 0 {
		buildInfoString, _ := json.Marshal(buildInfo)
		// Case no module was not created
		assert.Failf(t, "%s test with the command: \nrt bad %s \nexpected to have module with the following dependencies: \n%s \nbut has no modules: \n%s",
			buildInfoTestParams.description, buildInfoTestParams.commandArgs, buildInfoTestParams.expectedDependencies, buildInfoString)
	}
	// The checksums are ignored when comparing the actual and the expected
	assert.Equalf(t, len(buildInfoTestParams.expectedDependencies), len(buildInfo.Modules[0].Dependencies),
		"%s test with the command: \nrt bad %s  \nexpected to have the following dependencies: \n%s \nbut has: \n%s",
		buildInfoTestParams.description, buildInfoTestParams.commandArgs, buildInfoTestParams.expectedDependencies, dependenciesToPrintableArray(buildInfo.Modules[0].Dependencies))

	for _, expectedDependency := range buildInfoTestParams.expectedDependencies {
		found := false
		for _, actualDependency := range buildInfo.Modules[0].Dependencies {
			if actualDependency.Id == expectedDependency {
				found = true
				break
			}
		}
		// The checksums are ignored when comparing the actual and the expected
		assert.Truef(t, found, "%s test with the command: \nrt bad %s \nexpected to have the following dependencies: \n%s \nbut has: \n%s",
			buildInfoTestParams.description, buildInfoTestParams.commandArgs, buildInfoTestParams.expectedDependencies, dependenciesToPrintableArray(buildInfo.Modules[0].Dependencies))
	}
}

func dependenciesToPrintableArray(dependencies []buildinfo.Dependency) []string {
	ids := []string{}
	for _, dependency := range dependencies {
		ids = append(ids, dependency.Id)
	}
	return ids
}

type buildAddDepsBuildInfoTestParams struct {
	description          string
	commandArgs          []string
	expectedDependencies []string
	buildName            string
	buildNumber          string
	validationFunc       func(*testing.T, buildAddDepsBuildInfoTestParams)
}
