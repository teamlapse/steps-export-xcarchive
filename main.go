package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-steputils/output"
	"github.com/bitrise-io/go-steputils/stepconf"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/env"
	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-xcode/models"
	"github.com/bitrise-io/go-xcode/profileutil"
	"github.com/bitrise-io/go-xcode/utility"
	"github.com/bitrise-io/go-xcode/xcarchive"
	"github.com/bitrise-io/go-xcode/xcodebuild"
	"howett.net/plist"
)

const (
	bitriseIPAPthEnvKey                 = "BITRISE_IPA_PATH"
	bitriseDSYMPthEnvKey                = "BITRISE_DSYM_PATH"
	bitriseIDEDistributionLogsPthEnvKey = "BITRISE_IDEDISTRIBUTION_LOGS_PATH"
)

// Inputs ...
type Inputs struct {
	ArchivePath               string `env:"archive_path,dir"`
	DistributionMethod        string `env:"distribution_method,opt[development,app-store,ad-hoc,enterprise]"`
	UploadBitcode             bool   `env:"upload_bitcode,opt[yes,no]"`
	CompileBitcode            bool   `env:"compile_bitcode,opt[yes,no]"`
	TeamID                    string `env:"export_development_team"`
	ProductToDistribute       string `env:"product,opt[app,app-clip]"`
	ExportOptionsPlistContent string `env:"export_options_plist_content"`

	DeployDir  string `env:"BITRISE_DEPLOY_DIR"`
	VerboseLog bool   `env:"verbose_log,opt[yes,no]"`
}

type Config struct {
	ArchivePath               string
	DeployDir                 string
	ProductToDistribute       ExportProduct
	XcodebuildVersion         models.XcodebuildVersionModel
	ExportOptionsPlistContent string
	DistributionMethod        string
	TeamID                    string
	UploadBitcode             bool
	CompileBitcode            bool
}

type RunOpts struct {
	ArchivePath               string
	DeployDir                 string
	ProductToDistribute       ExportProduct
	XcodebuildVersion         models.XcodebuildVersionModel
	ExportOptionsPlistContent string
	DistributionMethod        string
	TeamID                    string
	UploadBitcode             bool
	CompileBitcode            bool
}

type RunOut struct {
	IDEDistrubutionLogDir string
	TmpDir                string
	AppDSYMs              []string
	ArchiveName           string
}

type ExportOpts struct {
	IDEDistrubutionLogDir string
	TmpDir                string
	DeployDir             string
	AppDSYMs              []string
	ArchiveName           string
}

type Step struct {
	commandFactory command.Factory
	inputParser    stepconf.InputParser
}

func (s Step) ProcessInputs() (Config, error) {
	var inputs Inputs
	if err := s.inputParser.Parse(&inputs); err != nil {
		return Config{}, fmt.Errorf("issue with input: %s", err)
	}

	productToDistribute, err := ParseExportProduct(inputs.ProductToDistribute)
	if err != nil {
		return Config{}, fmt.Errorf("failed to parse export product option, error: %s", err)
	}

	stepconf.Print(inputs)
	fmt.Println()

	trimmedExportOptions := strings.TrimSpace(inputs.ExportOptionsPlistContent)
	if inputs.ExportOptionsPlistContent != trimmedExportOptions {
		inputs.ExportOptionsPlistContent = trimmedExportOptions
		log.Warnf("ExportOptionsPlistContent contains leading and trailing white space, removed:")
		log.Printf(inputs.ExportOptionsPlistContent)
		fmt.Println()
	}
	if inputs.ExportOptionsPlistContent != "" {
		var options map[string]interface{}
		if _, err := plist.Unmarshal([]byte(inputs.ExportOptionsPlistContent), &options); err != nil {
			return Config{}, fmt.Errorf("issue with input ExportOptionsPlistContent: %s", err.Error())
		}
	}

	trimmedTeamID := strings.TrimSpace(inputs.TeamID)
	if inputs.TeamID != trimmedTeamID {
		inputs.TeamID = trimmedTeamID
		log.Warnf("TeamID contains leading and trailing white space, removed: %s", inputs.TeamID)
	}

	log.SetEnableDebugLog(inputs.VerboseLog)

	log.Infof("Step determined configs:")

	xcodebuildVersion, err := utility.GetXcodeVersion(s.commandFactory)
	if err != nil {
		return Config{}, fmt.Errorf("failed to determine Xcode version, error: %s", err)
	}
	log.Printf("- xcodebuildVersion: %s (%s)", xcodebuildVersion.Version, xcodebuildVersion.BuildVersion)

	return Config{
		ArchivePath:               inputs.ArchivePath,
		DeployDir:                 inputs.DeployDir,
		ProductToDistribute:       productToDistribute,
		XcodebuildVersion:         xcodebuildVersion,
		ExportOptionsPlistContent: inputs.ExportOptionsPlistContent,
		DistributionMethod:        inputs.DistributionMethod,
		TeamID:                    inputs.TeamID,
		UploadBitcode:             inputs.UploadBitcode,
		CompileBitcode:            inputs.CompileBitcode,
	}, nil
}

func (s Step) Run(opts RunOpts) (RunOut, error) {
	archiveExt := filepath.Ext(opts.ArchivePath)
	archiveName := filepath.Base(opts.ArchivePath)
	archiveName = strings.TrimSuffix(archiveName, archiveExt)
	exportOptionsPath := filepath.Join(opts.DeployDir, "export_options.plist")

	envsToUnset := []string{"GEM_HOME", "GEM_PATH", "RUBYLIB", "RUBYOPT", "BUNDLE_BIN_PATH", "_ORIGINAL_GEM_PATH", "BUNDLE_GEMFILE"}
	for _, key := range envsToUnset {
		if err := os.Unsetenv(key); err != nil {
			return RunOut{}, fmt.Errorf("failed to unset (%s), error: %s", key, err)
		}
	}

	archive, err := xcarchive.NewIosArchive(opts.ArchivePath)
	if err != nil {
		return RunOut{}, fmt.Errorf("failed to parse archive, error: %s", err)
	}

	mainApplication := archive.Application
	archiveExportMethod := mainApplication.ProvisioningProfile.ExportType
	archiveCodeSignIsXcodeManaged := profileutil.IsXcodeManaged(mainApplication.ProvisioningProfile.Name)

	if opts.ProductToDistribute == ExportProductAppClip {
		if opts.XcodebuildVersion.MajorVersion < 12 {
			return RunOut{}, fmt.Errorf("exporting an App Clip requires Xcode 12 or a later version")
		}

		if archive.Application.ClipApplication == nil {
			return RunOut{}, fmt.Errorf("failed to export App Clip, error: xcarchive does not contain an App Clip")
		}
	}

	fmt.Println()
	log.Infof("Archive info:")
	log.Printf("team: %s (%s)", mainApplication.ProvisioningProfile.TeamName, mainApplication.ProvisioningProfile.TeamID)
	log.Printf("profile: %s (%s)", mainApplication.ProvisioningProfile.Name, mainApplication.ProvisioningProfile.UUID)
	log.Printf("export: %s", archiveExportMethod)
	log.Printf("Xcode managed profile: %v", archiveCodeSignIsXcodeManaged)
	fmt.Println()

	log.Infof("Exporting with export options...")

	if opts.ExportOptionsPlistContent != "" {
		log.Printf("Export options content provided, using it:")
		fmt.Println(opts.ExportOptionsPlistContent)

		if err := fileutil.WriteStringToFile(exportOptionsPath, opts.ExportOptionsPlistContent); err != nil {
			return RunOut{}, fmt.Errorf("failed to write export options to file, error: %s", err)
		}
	} else {
		exportOptionsContent, err := generateExportOptionsPlist(opts.ProductToDistribute, opts.DistributionMethod, opts.TeamID, opts.UploadBitcode, opts.CompileBitcode, opts.XcodebuildVersion.MajorVersion, archive)
		if err != nil {
			return RunOut{}, fmt.Errorf("failed to generate export options, error: %s", err)
		}

		log.Printf("\ngenerated export options content:\n%s", exportOptionsContent)

		if err := fileutil.WriteStringToFile(exportOptionsPath, exportOptionsContent); err != nil {
			return RunOut{}, fmt.Errorf("failed to write export options to file, error: %s", err)
		}

		fmt.Println()
	}

	tmpDir, err := pathutil.NormalizedOSTempDirPath("__export__")
	if err != nil {
		return RunOut{}, fmt.Errorf("failed to create tmp dir, error: %s", err)
	}

	exportCmd := xcodebuild.NewExportCommand(s.commandFactory)
	exportCmd.SetArchivePath(opts.ArchivePath)
	exportCmd.SetExportDir(tmpDir)
	exportCmd.SetExportOptionsPlist(exportOptionsPath)

	log.Donef("$ %s", exportCmd.PrintableCmd())
	fmt.Println()

	var ideDistrubutionLogDir string
	if xcodebuildOut, err := exportCmd.RunAndReturnOutput(); err != nil {
		// xcdistributionlogs
		if logsDirPth, err := findIDEDistrubutionLogsPath(xcodebuildOut); err != nil {
			log.Warnf("Failed to find xcdistributionlogs, error: %s", err)
		} else {
			ideDistrubutionLogDir = logsDirPth
			log.Warnf(`If you can't find the reason of the error in the log, please check the xcdistributionlogs
The logs directory will be stored in $BITRISE_DEPLOY_DIR, and its full path
will be available in the $BITRISE_IDEDISTRIBUTION_LOGS_PATH environment variable`)
		}

		return RunOut{
			IDEDistrubutionLogDir: ideDistrubutionLogDir,
		}, fmt.Errorf("export failed, error: %s", err)
	}

	appDSYMs, _, err := archive.FindDSYMs()
	if err != nil {
		return RunOut{}, fmt.Errorf("failed to export dsym, error: %s", err)
	}

	return RunOut{
		IDEDistrubutionLogDir: ideDistrubutionLogDir,
		TmpDir:                tmpDir,
		AppDSYMs:              appDSYMs,
		ArchiveName:           archiveName,
	}, nil
}

func (s Step) ExportOutput(opts ExportOpts) error {
	if opts.IDEDistrubutionLogDir != "" {
		ideDistributionLogsZipPath := filepath.Join(opts.DeployDir, "xcodebuild.xcdistributionlogs.zip")
		if err := output.ZipAndExportOutput([]string{opts.IDEDistrubutionLogDir}, ideDistributionLogsZipPath, bitriseIDEDistributionLogsPthEnvKey); err != nil {
			log.Warnf("Failed to export %s, error: %s", bitriseIDEDistributionLogsPthEnvKey, err)
		}

		return nil
	}

	exportedIPAPath := ""
	pattern := filepath.Join(opts.TmpDir, "*.ipa")
	ipas, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to collect ipa files, error: %s", err)
	}

	if len(ipas) == 0 {
		return fmt.Errorf("no ipa found with pattern: %s", pattern)
	} else if len(ipas) == 1 {
		exportedIPAPath = filepath.Join(opts.DeployDir, filepath.Base(ipas[0]))
		if err := command.CopyFile(ipas[0], exportedIPAPath); err != nil {
			return fmt.Errorf("failed to copy (%s) -> (%s), error: %s", ipas[0], exportedIPAPath, err)
		}
	} else {
		log.Warnf("More than 1 .ipa file found")

		for _, ipa := range ipas {
			base := filepath.Base(ipa)
			deployPth := filepath.Join(opts.DeployDir, base)

			if err := command.CopyFile(ipa, deployPth); err != nil {
				return fmt.Errorf("failed to copy (%s) -> (%s), error: %s", ipas[0], ipa, err)
			}
			exportedIPAPath = ipa
		}
	}

	if err := output.ExportOutputFile(exportedIPAPath, exportedIPAPath, bitriseIPAPthEnvKey); err != nil {
		return fmt.Errorf("failed to export %s, error: %s", bitriseIPAPthEnvKey, err)
	}

	log.Donef("The ipa path is now available in the Environment Variable: %s (value: %s)", bitriseIPAPthEnvKey, exportedIPAPath)

	if len(opts.AppDSYMs) == 0 {
		log.Warnf("No dSYM was found in the archive")
		return nil
	}

	dsymZipPath := filepath.Join(opts.DeployDir, opts.ArchiveName+".dSYM.zip")
	if err := output.ZipAndExportOutput(opts.AppDSYMs, dsymZipPath, bitriseDSYMPthEnvKey); err != nil {
		return fmt.Errorf("failed to export %s, error: %s", bitriseDSYMPthEnvKey, err)
	}

	log.Donef("The dSYM zip path is now available in the Environment Variable: %s (value: %s)", bitriseDSYMPthEnvKey, dsymZipPath)

	return nil
}

func RunStep() error {
	envRepository := env.NewRepository()

	step := Step{
		commandFactory: command.NewFactory(envRepository),
		inputParser:    stepconf.NewInputParser(envRepository),
	}

	config, err := step.ProcessInputs()
	if err != nil {
		return err
	}

	runOpts := RunOpts(config)
	out, runErr := step.Run(runOpts)

	exportOpts := ExportOpts{
		IDEDistrubutionLogDir: out.IDEDistrubutionLogDir,
		TmpDir:                out.TmpDir,
		DeployDir:             config.DeployDir,
		AppDSYMs:              out.AppDSYMs,
		ArchiveName:           out.ArchiveName,
	}
	exportErr := step.ExportOutput(exportOpts)

	if runErr != nil {
		return runErr
	}
	if exportErr != nil {
		return exportErr
	}

	return nil
}

func main() {
	if err := RunStep(); err != nil {
		log.Errorf(err.Error())
		os.Exit(1)
	}
}
