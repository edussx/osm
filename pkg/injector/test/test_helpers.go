// Package test implements utility routes to test the functionality provided by the injector package.
package test

import (
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gopkg.in/yaml.v2"

	"github.com/openservicemesh/osm/pkg/logger"
)

// All the YAML files listed above are in this sub-directory
const directoryForExpectationsYAML = "../../tests/envoy_xds_expectations/"

var log = logger.New("sidecar-injector")

func getTempDir() string {
	dir, err := ioutil.TempDir("", "osm_test_envoy")
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating temp directory")
	}
	return dir
}

// LoadExpectedEnvoyYAML loads the expectation for a given test from the file system. This must run within ginkgo.It()
func LoadExpectedEnvoyYAML(expectationFilePath string) string {
	// The expectationFileName will contain the name of the function by convention
	log.Info().Msgf("Loading test expectation from %s", filepath.Clean(expectationFilePath))
	expectedEnvoyConfig, err := ioutil.ReadFile(filepath.Clean(expectationFilePath))
	if err != nil {
		log.Err(err).Msgf("Error reading expected Envoy bootstrap YAML from file %s", expectationFilePath)
	}
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return string(expectedEnvoyConfig)
}

// MarshalXdsStructAndSaveToFile converts a an xDS struct into YAML and saves it to a file. This must run within ginkgo.It()
func MarshalXdsStructAndSaveToFile(m protoreflect.ProtoMessage, filePath string) string {
	marshalOptions := protojson.MarshalOptions{
		UseProtoNames: true,
	}
	configJSON, err := marshalOptions.Marshal(m)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Convert the JSON to an object.
	var jsonObj interface{}
	// We are using yaml.Unmarshal here (instead of json.Unmarshal) because the
	// Go JSON library doesn't try to pick the right number type (int, float,
	// etc.) when unmarshalling to interface{}, it just picks float64
	// universally. go-yaml does go through the effort of picking the right
	// number type, so we can preserve number type throughout this process.
	err = yaml.Unmarshal([]byte(configJSON), &jsonObj)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Marshal this object into YAML.
	configYAML, err := yaml.Marshal(jsonObj)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	log.Info().Msgf("Saving %s...", filePath)
	err = ioutil.WriteFile(filepath.Clean(filePath), configYAML, 0600)
	if err != nil {
		log.Err(err).Msgf("Error writing actual Envoy Cluster XDS YAML to file %s", filePath)
	}
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return string(configYAML)
}

// MarshalAndSaveToFile converts a generic Go struct into YAML and saves it to a file. This must run within ginkgo.It()
func MarshalAndSaveToFile(someStruct interface{}, filePath string) string {
	fileContent, err := yaml.Marshal(someStruct)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	log.Info().Msgf("Saving %s...", filePath)
	err = ioutil.WriteFile(filepath.Clean(filePath), fileContent, 0600)
	if err != nil {
		log.Err(err).Msgf("Error writing actual Envoy Cluster XDS YAML to file %s", filePath)
	}
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return string(fileContent)
}

// ThisFunction runs the given function in a ginkgo.Context(), marshals the output and compares to an expectation loaded from file.
func ThisFunction(functionName string, fn func() interface{}) {
	ginkgo.Context(fmt.Sprintf("ThisFunction %s", functionName), func() {
		ginkgo.It("creates Envoy config", func() {
			expectationFilePath := path.Join(directoryForExpectationsYAML, fmt.Sprintf("expected_output_%s.yaml", functionName))
			actualFilePath := path.Join(getTempDir(), fmt.Sprintf("actual_output_%s.yaml", functionName))
			log.Info().Msgf("Actual output of %s is going to be saved in %s", functionName, actualFilePath)
			actual := fn()

			expectedYAML := LoadExpectedEnvoyYAML(expectationFilePath)
			actualYAML := MarshalAndSaveToFile(actual, actualFilePath)

			Compare(functionName, actualFilePath, expectationFilePath, actualYAML, expectedYAML)
		})
	})
}

// ThisXdsClusterFunction runs the given function in a ginkgo.Context(), marshals the output and compares to an expectation loaded from file.
func ThisXdsClusterFunction(functionName string, fn func() protoreflect.ProtoMessage) {
	ginkgo.Context(fmt.Sprintf("ThisFunction %s", functionName), func() {
		ginkgo.It("creates Envoy config", func() {
			expectationFilePath := path.Join(directoryForExpectationsYAML, fmt.Sprintf("expected_output_%s.yaml", functionName))
			actualFilePath := path.Join(getTempDir(), fmt.Sprintf("actual_output_%s.yaml", functionName))
			log.Info().Msgf("Actual output of %s is going to be saved in %s", functionName, actualFilePath)
			actual := fn()

			expectedYAML := LoadExpectedEnvoyYAML(expectationFilePath)
			actualYAML := MarshalXdsStructAndSaveToFile(actual, actualFilePath)

			Compare(functionName, actualFilePath, expectationFilePath, actualYAML, expectedYAML)
		})
	})
}

// ThisXdsListenerFunction runs the given function in a ginkgo.Context(), marshals the output and compares to an expectation loaded from file.
func ThisXdsListenerFunction(functionName string, fn func() (protoreflect.ProtoMessage, error)) {
	ginkgo.Context(fmt.Sprintf("ThisFunction %s", functionName), func() {
		ginkgo.It("creates Envoy config", func() {
			expectationFilePath := path.Join(directoryForExpectationsYAML, fmt.Sprintf("expected_output_%s.yaml", functionName))
			actualFilePath := path.Join(getTempDir(), fmt.Sprintf("actual_output_%s.yaml", functionName))
			log.Info().Msgf("Actual output of %s is going to be saved in %s", functionName, actualFilePath)
			actual, err := fn()
			gomega.Expect(err).To(gomega.BeNil())

			expectedYAML := LoadExpectedEnvoyYAML(expectationFilePath)
			actualYAML := MarshalXdsStructAndSaveToFile(actual, actualFilePath)

			Compare(functionName, actualFilePath, expectationFilePath, actualYAML, expectedYAML)
		})
	})
}

// Compare is a wrapper around gomega.Expect().To(Equal()) and compares actualYAML and expectedYAML; It also provides a verbose message when things don't match with a tip on how to fix things.
func Compare(functionName, actualFilename, expectedFilename, actualYAML, expectedYAML string) {
	gomega.Expect(actualYAML).To(gomega.Equal(expectedYAML),
		fmt.Sprintf(`The actual output of function %s (saved in file %s) does not match the expected loaded from file %s;
Compare the contents of the files with "diff %s %s"
If you are certain the actual output is correct: "cat %s > %s"`,
			functionName, actualFilename, expectedFilename,
			actualFilename, expectedFilename,
			actualFilename, expectedFilename))
}
