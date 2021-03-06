// +build darwin

package service

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	logging "github.com/remoteit/systemkit-logging"
	encoders "github.com/remoteit/systemkit-service-encoders-launchd"
	spec "github.com/remoteit/systemkit-service-spec"
	"github.com/remoteit/systemkit-service/helpers"
)

var logTag = "LaunchD-SERVICE"

type launchdService struct {
	serviceSpec            spec.SERVICE
	useConfigAsFileContent bool
	fileContentTemplate    string
}

func newServiceFromSERVICE(serviceSpec spec.SERVICE) Service {
	// override some values - platform specific
	// https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html
	logDir := filepath.Join(helpers.HomeDir(""), "Library/Logs", serviceSpec.Name)
	if helpers.IsRoot() {
		logDir = filepath.Join("/Library/Logs", serviceSpec.Name)
	}

	if serviceSpec.Logging.StdOut.UseDefault {
		serviceSpec.Logging.StdOut.Value = filepath.Join(logDir, serviceSpec.Name+".stdout.log")
	}

	if serviceSpec.Logging.StdErr.UseDefault {
		serviceSpec.Logging.StdErr.Value = filepath.Join(logDir, serviceSpec.Name+".stderr.log")
	}

	logging.Debugf("%s: serviceSpec object: %s", logTag, helpers.AsJSONString(serviceSpec))

	launchdService := &launchdService{
		serviceSpec:            serviceSpec,
		useConfigAsFileContent: true,
	}

	return launchdService
}

func newServiceFromName(name string) (Service, error) {
	serviceFile := filepath.Join(helpers.HomeDir(""), "Library/LaunchAgents", name+".plist")
	if helpers.IsRoot() {
		serviceFile = filepath.Join("/Library/LaunchDaemons", name+".plist")
	}

	fileContent, err := ioutil.ReadFile(serviceFile)
	if err != nil {
		return nil, ErrServiceDoesNotExist
	}

	return newServiceFromPlatformTemplate(name, string(fileContent))
}

func newServiceFromPlatformTemplate(name string, template string) (Service, error) {
	logging.Debugf("%s: template: %s", logTag, template)

	return &launchdService{
		serviceSpec:            encoders.LaunchDToSERVICE(template),
		useConfigAsFileContent: false,
		fileContentTemplate:    template,
	}, nil
}

func (thisRef launchdService) Install() error {
	dir := filepath.Dir(thisRef.filePath())

	// 1.
	logging.Debugf("%s: making sure folder exists: %s", logTag, dir)
	os.MkdirAll(dir, os.ModePerm)

	// 2.
	logging.Debugf("%s: generating plist file", logTag)
	fileContent := encoders.SERVICEToLaunchD(thisRef.serviceSpec)

	logging.Debugf("%s: writing plist to: %s", logTag, thisRef.filePath())
	err := ioutil.WriteFile(thisRef.filePath(), []byte(fileContent), 0644)
	if err != nil {
		return err
	}

	logging.Debugf("%s: wrote unit: %s", logTag, string(fileContent))

	return nil
}

func (thisRef launchdService) Uninstall() error {
	// 1.
	err := thisRef.Stop()
	if err != nil && !helpers.Is(err, ErrServiceDoesNotExist) {
		return err
	}

	// 2.
	logging.Debugf("%s: remove plist file: %s", logTag, thisRef.filePath())
	err = os.Remove(thisRef.filePath())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such file or directory") {
			return nil
		}

		return err
	}

	// INFO: ignore the return value as is it is barely defined by the docs
	// what the expected behavior would be. The previous stop and remove the "plist" file
	// will uninstall the service.
	runLaunchCtlCommand("remove", thisRef.serviceSpec.Name)
	return nil
}

func (thisRef launchdService) Start() error {
	// 1.
	output, _ := runLaunchCtlCommand("load", "-w", thisRef.filePath())
	if strings.Contains(output, "No such file or directory") {
		return ErrServiceDoesNotExist
	} else if strings.Contains(output, "Invalid property list") {
		return ErrServiceConfigError
	}

	if strings.Contains(output, "service already loaded") {
		logging.Debugf("service already loaded")

		return nil
	}

	runLaunchCtlCommand("start", thisRef.serviceSpec.Name)
	return nil
}

func (thisRef launchdService) Stop() error {
	runLaunchCtlCommand("stop", thisRef.serviceSpec.Name)
	output, err := runLaunchCtlCommand("unload", thisRef.filePath())
	if strings.Contains(output, "Could not find specified service") {
		return ErrServiceDoesNotExist
	}

	return err
}

func (thisRef launchdService) Info() Info {
	fileContent, fileContentErr := ioutil.ReadFile(thisRef.filePath())

	result := Info{
		Error:       nil,
		Service:     thisRef.serviceSpec,
		IsRunning:   false,
		PID:         -1,
		FilePath:    thisRef.filePath(),
		FileContent: string(fileContent),
	}

	if fileContentErr != nil || len(fileContent) <= 0 {
		result.Error = ErrServiceDoesNotExist
	}

	output, err := runLaunchCtlCommand("list")
	if err != nil {
		result.Error = err
		logging.Errorf("error getting launchctl status: %s", err)
		return result
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		chunks := strings.Split(line, "\t")

		if chunks[2] == thisRef.serviceSpec.Name {
			if chunks[0] != "-" {
				pid, _ := strconv.Atoi(chunks[0])
				result.PID = pid
			}

			if result.PID != -1 {
				result.IsRunning = true
			}

			break
		}
	}

	return result
}

func (thisRef launchdService) filePath() string {
	if helpers.IsRoot() {
		return filepath.Join("/Library/LaunchDaemons", thisRef.serviceSpec.Name+".plist")
	}

	return filepath.Join(helpers.HomeDir(""), "Library/LaunchAgents", thisRef.serviceSpec.Name+".plist")
}

func runLaunchCtlCommand(args ...string) (string, error) {
	// if !helpers.IsRoot() {
	// 	args = append([]string{"--user"}, args...)
	// }

	logging.Debugf("%s: RUN-LAUNCHCTL: launchctl %s", logTag, strings.Join(args, " "))

	output, err := helpers.ExecWithArgs("launchctl", args...)
	errAsString := ""
	if err != nil {
		errAsString = err.Error()
	}

	logging.Debugf("%s: RUN-LAUNCHCTL-OUT: output: %s, error: %s", logTag, output, errAsString)

	return output, err
}
