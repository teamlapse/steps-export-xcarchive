package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitrise-io/go-steputils/output"
	"github.com/bitrise-io/go-steputils/v2/stepconf"
	v1command "github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/fileutil"
	v1log "github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-utils/v2/command"
	"github.com/bitrise-io/go-utils/v2/env"
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/go-utils/v2/retryhttp"
	"github.com/bitrise-io/go-xcode/devportalservice"
	"github.com/bitrise-io/go-xcode/models"
	"github.com/bitrise-io/go-xcode/profileutil"
	"github.com/bitrise-io/go-xcode/utility"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/certdownloader"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/codesignasset"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/devportalclient"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/localcodesignasset"
	"github.com/bitrise-io/go-xcode/v2/autocodesign/profiledownloader"
	"github.com/bitrise-io/go-xcode/v2/codesign"
	"github.com/bitrise-io/go-xcode/v2/xcarchive"
	"github.com/bitrise-io/go-xcode/xcodebuild"
	"howett.net/plist"
)

const (
	// Outputs
	bitriseIPAPthEnvKey                 = "BITRISE_IPA_PATH"
	bitriseDSYMPthEnvKey                = "BITRISE_DSYM_PATH"
	bitriseIDEDistributionLogsPthEnvKey = "BITRISE_IDEDISTRIBUTION_LOGS_PATH"
	// Code Signing Authentication Source
	codeSignSourceOff     = "off"
	codeSignSourceAPIKey  = "api-key"
	codeSignSourceAppleID = "apple-id"
)

// Inputs ...
type Inputs struct {
	ArchivePath         string `env:"archive_path,dir"`
	ProductToDistribute string `env:"product,opt[app,app-clip]"`
	DistributionMethod  string `env:"distribution_method,opt[development,app-store,ad-hoc,enterprise]"`
	// Automatic code signing
	CodeSigningAuthSource     string          `env:"automatic_code_signing,opt[off,api-key,apple-id]"`
	CertificateURLList        string          `env:"certificate_url_list"`
	CertificatePassphraseList stepconf.Secret `env:"passphrase_list"`
	KeychainPath              string          `env:"keychain_path"`
	KeychainPassword          stepconf.Secret `env:"keychain_password"`
	RegisterTestDevices       bool            `env:"register_test_devices,opt[yes,no]"`
	TestDeviceListPath        string          `env:"test_device_list_path"`
	MinDaysProfileValid       int             `env:"min_profile_validity,required"`
	BuildURL                  string          `env:"BITRISE_BUILD_URL"`
	BuildAPIToken             stepconf.Secret `env:"BITRISE_BUILD_API_TOKEN"`
	// IPA export configuration
	TeamID                      string `env:"export_development_team"`
	CompileBitcode              bool   `env:"compile_bitcode,opt[yes,no]"`
	UploadBitcode               bool   `env:"upload_bitcode,opt[yes,no]"`
	ManageVersionAndBuildNumber bool   `env:"manage_version_and_build_number"`
	ExportOptionsPlistContent   string `env:"export_options_plist_content"`
	// App Store Connect connection override
	APIKeyPath     stepconf.Secret `env:"api_key_path"`
	APIKeyID       string          `env:"api_key_id"`
	APIKeyIssuerID string          `env:"api_key_issuer_id"`
	// Debugging
	VerboseLog bool `env:"verbose_log,opt[yes,no]"`
	// Output export
	DeployDir string `env:"BITRISE_DEPLOY_DIR"`
}

type Config struct {
	ArchivePath                 string
	DeployDir                   string
	ProductToDistribute         ExportProduct
	ExportOptionsPlistContent   string
	DistributionMethod          string
	TeamID                      string
	UploadBitcode               bool
	CompileBitcode              bool
	ManageVersionAndBuildNumber bool
	XcodebuildVersion           models.XcodebuildVersionModel
	CodesignManager             *codesign.Manager // nil if automatic code signing is "off"
	VerboseLog                  bool
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
	logger         log.Logger
}

func (s Step) ProcessInputs() (Config, error) {
	var inputs Inputs
	if err := s.inputParser.Parse(&inputs); err != nil {
		return Config{}, fmt.Errorf("issue with input: %s", err)
	}

	v1log.SetEnableDebugLog(inputs.VerboseLog)
	s.logger.EnableDebugLog(inputs.VerboseLog)

	productToDistribute, err := ParseExportProduct(inputs.ProductToDistribute)
	if err != nil {
		return Config{}, fmt.Errorf("failed to parse export product option, error: %s", err)
	}

	stepconf.Print(inputs)
	fmt.Println()

	trimmedExportOptions := strings.TrimSpace(inputs.ExportOptionsPlistContent)
	if inputs.ExportOptionsPlistContent != trimmedExportOptions {
		inputs.ExportOptionsPlistContent = trimmedExportOptions
		s.logger.Warnf("ExportOptionsPlistContent contains leading and trailing white space, removed:")
		s.logger.Printf(inputs.ExportOptionsPlistContent)
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
		s.logger.Warnf("TeamID contains leading and trailing white space, removed: %s", inputs.TeamID)
	}

	s.logger.Infof("Step determined configs:")

	xcodebuildVersion, err := utility.GetXcodeVersion()
	if err != nil {
		return Config{}, fmt.Errorf("failed to determine Xcode version, error: %s", err)
	}
	s.logger.Printf("- xcodebuildVersion: %s (%s)", xcodebuildVersion.Version, xcodebuildVersion.BuildVersion)

	var codesignManager *codesign.Manager
	if inputs.CodeSigningAuthSource != codeSignSourceOff {
		manager, err := s.createCodesignManager(inputs, int(xcodebuildVersion.MajorVersion))
		if err != nil {
			return Config{}, err
		}
		codesignManager = &manager
	}

	return Config{
		ArchivePath:               inputs.ArchivePath,
		DeployDir:                 inputs.DeployDir,
		ProductToDistribute:       productToDistribute,
		ExportOptionsPlistContent: inputs.ExportOptionsPlistContent,
		DistributionMethod:        inputs.DistributionMethod,
		TeamID:                    inputs.TeamID,
		UploadBitcode:             inputs.UploadBitcode,
		CompileBitcode:            inputs.CompileBitcode,
		XcodebuildVersion:         xcodebuildVersion,
		CodesignManager:           codesignManager,
	}, nil
}

func (s Step) createCodesignManager(inputs Inputs, xcodeMajorVersion int) (codesign.Manager, error) {
	var authType codesign.AuthType
	switch inputs.CodeSigningAuthSource {
	case codeSignSourceAppleID:
		authType = codesign.AppleIDAuth
	case codeSignSourceAPIKey:
		authType = codesign.APIKeyAuth
	case codeSignSourceOff:
		return codesign.Manager{}, fmt.Errorf("automatic code signing is disabled")
	}

	codesignInputs := codesign.Input{
		AuthType:                  authType,
		DistributionMethod:        inputs.DistributionMethod,
		CertificateURLList:        inputs.CertificateURLList,
		CertificatePassphraseList: inputs.CertificatePassphraseList,
		KeychainPath:              inputs.KeychainPath,
		KeychainPassword:          inputs.KeychainPassword,
	}

	codesignConfig, err := codesign.ParseConfig(codesignInputs, s.commandFactory)
	if err != nil {
		return codesign.Manager{}, fmt.Errorf("issue with input: %s", err)
	}

	a, err := xcarchive.NewIosArchive(inputs.ArchivePath)
	if err != nil {
		return codesign.Manager{}, err
	}
	archive := codesign.NewArchive(a)

	var serviceConnection *devportalservice.AppleDeveloperConnection = nil
	devPortalClientFactory := devportalclient.NewFactory(s.logger)
	if inputs.BuildURL != "" && inputs.BuildAPIToken != "" {
		if serviceConnection, err = devPortalClientFactory.CreateBitriseConnection(inputs.BuildURL, string(inputs.BuildAPIToken)); err != nil {
			return codesign.Manager{}, err
		}
	}

	connectionInputs := codesign.ConnectionOverrideInputs{
		APIKeyPath:     inputs.APIKeyPath,
		APIKeyID:       inputs.APIKeyID,
		APIKeyIssuerID: inputs.APIKeyIssuerID,
	}

	appleAuthCredentials, err := codesign.SelectConnectionCredentials(authType, serviceConnection, connectionInputs, s.logger)
	if err != nil {
		return codesign.Manager{}, err
	}

	opts := codesign.Opts{
		AuthType:                   authType,
		ShouldConsiderXcodeSigning: true,
		TeamID:                     inputs.TeamID,
		ExportMethod:               codesignConfig.DistributionMethod,
		XcodeMajorVersion:          xcodeMajorVersion,
		RegisterTestDevices:        inputs.RegisterTestDevices,
		SignUITests:                false,
		MinDaysProfileValidity:     inputs.MinDaysProfileValid,
		IsVerboseLog:               inputs.VerboseLog,
	}

	var testDevices []devportalservice.TestDevice
	if inputs.TestDeviceListPath != "" {
		testDevices, err = devportalservice.ParseTestDevicesFromFile(inputs.TestDeviceListPath, time.Now())
		if err != nil {
			return codesign.Manager{}, fmt.Errorf("failed to process device list (%s): %s", inputs.TestDeviceListPath, err)
		}
	} else if serviceConnection != nil {
		testDevices = serviceConnection.TestDevices
	}

	return codesign.NewManagerWithArchive(
		opts,
		appleAuthCredentials,
		testDevices,
		devPortalClientFactory,
		certdownloader.NewDownloader(codesignConfig.CertificatesAndPassphrases, retry.NewHTTPClient().StandardClient()),
		profiledownloader.New([]string{}, retryhttp.NewClient(s.logger).StandardClient()), // not supported for now
		codesignasset.NewWriter(codesignConfig.Keychain),
		localcodesignasset.NewManager(localcodesignasset.NewProvisioningProfileProvider(), localcodesignasset.NewProvisioningProfileConverter()),
		archive,
		s.logger,
	), nil
}

func (s Step) Run(opts Config) (RunOut, error) {
	var authOptions *xcodebuild.AuthenticationParams = nil
	if opts.CodesignManager != nil {
		s.logger.Infof("Preparing code signing assets (certificates, profiles)")

		xcodebuildAuthParams, err := opts.CodesignManager.PrepareCodesigning()
		if err != nil {
			return RunOut{}, fmt.Errorf("failed to manage code signing: %s", err)
		}

		if xcodebuildAuthParams != nil {
			privateKey, err := xcodebuildAuthParams.WritePrivateKeyToFile()
			if err != nil {
				return RunOut{}, err
			}

			defer func() {
				if err := os.Remove(privateKey); err != nil {
					s.logger.Warnf("failed to remove private key file: %s", err)
				}
			}()

			authOptions = &xcodebuild.AuthenticationParams{
				KeyID:     xcodebuildAuthParams.KeyID,
				IsssuerID: xcodebuildAuthParams.IssuerID,
				KeyPath:   privateKey,
			}
		}
	} else {
		s.logger.Infof("Automatic code signing is disabled, skipped downloading code sign assets")
	}
	fmt.Println()

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
	s.logger.Infof("Archive info:")
	s.logger.Printf("team: %s (%s)", mainApplication.ProvisioningProfile.TeamName, mainApplication.ProvisioningProfile.TeamID)
	s.logger.Printf("profile: %s (%s)", mainApplication.ProvisioningProfile.Name, mainApplication.ProvisioningProfile.UUID)
	s.logger.Printf("export: %s", archiveExportMethod)
	s.logger.Printf("Xcode managed profile: %v", archiveCodeSignIsXcodeManaged)
	fmt.Println()

	s.logger.Infof("Exporting with export options...")

	if opts.ExportOptionsPlistContent != "" {
		s.logger.Printf("Export options content provided, using it:")
		fmt.Println(opts.ExportOptionsPlistContent)

		if err := fileutil.WriteStringToFile(exportOptionsPath, opts.ExportOptionsPlistContent); err != nil {
			return RunOut{}, fmt.Errorf("failed to write export options to file, error: %s", err)
		}
	} else {
		exportOptionsContent, err := generateExportOptionsPlist(opts.ProductToDistribute, opts.DistributionMethod, opts.TeamID, opts.UploadBitcode, opts.CompileBitcode, opts.XcodebuildVersion.MajorVersion, archive, opts.ManageVersionAndBuildNumber)
		if err != nil {
			return RunOut{}, fmt.Errorf("failed to generate export options, error: %s", err)
		}

		s.logger.Printf("\ngenerated export options content:\n%s", exportOptionsContent)

		if err := fileutil.WriteStringToFile(exportOptionsPath, exportOptionsContent); err != nil {
			return RunOut{}, fmt.Errorf("failed to write export options to file, error: %s", err)
		}

		fmt.Println()
	}

	tmpDir, err := pathutil.NormalizedOSTempDirPath("__export__")
	if err != nil {
		return RunOut{}, fmt.Errorf("failed to create tmp dir, error: %s", err)
	}

	exportCmd := xcodebuild.NewExportCommand()
	exportCmd.SetArchivePath(opts.ArchivePath)
	exportCmd.SetExportDir(tmpDir)
	exportCmd.SetExportOptionsPlist(exportOptionsPath)
	if authOptions != nil {
		exportCmd.SetAuthentication(*authOptions)
	}

	s.logger.Donef("$ %s", exportCmd.PrintableCmd())
	fmt.Println()

	var ideDistrubutionLogDir string
	if xcodebuildOut, err := exportCmd.RunAndReturnOutput(); err != nil {
		// xcdistributionlogs
		if logsDirPth, err := findIDEDistrubutionLogsPath(xcodebuildOut); err != nil {
			s.logger.Warnf("Failed to find xcdistributionlogs, error: %s", err)
		} else {
			ideDistrubutionLogDir = logsDirPth
			s.logger.Warnf(`If you can't find the reason of the error in the log, please check the xcdistributionlogs
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
			s.logger.Warnf("Failed to export %s, error: %s", bitriseIDEDistributionLogsPthEnvKey, err)
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
		if err := v1command.CopyFile(ipas[0], exportedIPAPath); err != nil {
			return fmt.Errorf("failed to copy (%s) -> (%s), error: %s", ipas[0], exportedIPAPath, err)
		}
	} else {
		s.logger.Warnf("More than 1 .ipa file found")

		for _, ipa := range ipas {
			base := filepath.Base(ipa)
			deployPth := filepath.Join(opts.DeployDir, base)

			if err := v1command.CopyFile(ipa, deployPth); err != nil {
				return fmt.Errorf("failed to copy (%s) -> (%s), error: %s", ipas[0], ipa, err)
			}
			exportedIPAPath = ipa
		}
	}

	if err := output.ExportOutputFile(exportedIPAPath, exportedIPAPath, bitriseIPAPthEnvKey); err != nil {
		return fmt.Errorf("failed to export %s, error: %s", bitriseIPAPthEnvKey, err)
	}

	s.logger.Donef("The ipa path is now available in the Environment Variable: %s (value: %s)", bitriseIPAPthEnvKey, exportedIPAPath)

	if len(opts.AppDSYMs) == 0 {
		s.logger.Warnf("No dSYM was found in the archive")
		return nil
	}

	dsymZipPath := filepath.Join(opts.DeployDir, opts.ArchiveName+".dSYM.zip")
	if err := output.ZipAndExportOutput(opts.AppDSYMs, dsymZipPath, bitriseDSYMPthEnvKey); err != nil {
		return fmt.Errorf("failed to export %s, error: %s", bitriseDSYMPthEnvKey, err)
	}

	s.logger.Donef("The dSYM zip path is now available in the Environment Variable: %s (value: %s)", bitriseDSYMPthEnvKey, dsymZipPath)

	return nil
}

func RunStep() error {
	envRepository := env.NewRepository()

	step := Step{
		commandFactory: command.NewFactory(envRepository),
		inputParser:    stepconf.NewInputParser(envRepository),
		logger:         log.NewLogger(),
	}

	config, err := step.ProcessInputs()
	if err != nil {
		step.logger.Errorf(err.Error())
		return err
	}

	out, runErr := step.Run(config)

	exportOpts := ExportOpts{
		IDEDistrubutionLogDir: out.IDEDistrubutionLogDir,
		TmpDir:                out.TmpDir,
		DeployDir:             config.DeployDir,
		AppDSYMs:              out.AppDSYMs,
		ArchiveName:           out.ArchiveName,
	}
	exportErr := step.ExportOutput(exportOpts)

	if runErr != nil {
		step.logger.Errorf(runErr.Error())
		return runErr
	}
	if exportErr != nil {
		step.logger.Errorf(exportErr.Error())
		return exportErr
	}

	return nil
}

func main() {
	if err := RunStep(); err != nil {
		os.Exit(1)
	}
}
