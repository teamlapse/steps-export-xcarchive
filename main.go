package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/steps-xcode-archive/utils"
	"github.com/bitrise-tools/go-xcode/certificateutil"
	"github.com/bitrise-tools/go-xcode/export"
	"github.com/bitrise-tools/go-xcode/exportoptions"
	"github.com/bitrise-tools/go-xcode/profileutil"
	"github.com/bitrise-tools/go-xcode/utility"
	"github.com/bitrise-tools/go-xcode/xcarchive"
	"github.com/bitrise-tools/go-xcode/xcodebuild"
)

const (
	bitriseIPAPthEnvKey                 = "BITRISE_IPA_PATH"
	bitriseDSYMPthEnvKey                = "BITRISE_DSYM_PATH"
	bitriseIDEDistributionLogsPthEnvKey = "BITRISE_IDEDISTRIBUTION_LOGS_PATH"
)

// ConfigsModel ...
type ConfigsModel struct {
	ArchivePath string

	ExportMethod                    string
	UploadBitcode                   string
	CompileBitcode                  string
	TeamID                          string
	CustomExportOptionsPlistContent string

	UseLegacyExport                     string
	LegacyExportProvisioningProfileName string

	DeployDir  string
	VerboseLog string
}

func createConfigsModelFromEnvs() ConfigsModel {
	return ConfigsModel{
		ArchivePath: os.Getenv("archive_path"),

		ExportMethod:   os.Getenv("export_method"),
		UploadBitcode:  os.Getenv("upload_bitcode"),
		CompileBitcode: os.Getenv("compile_bitcode"),
		TeamID:         os.Getenv("team_id"),
		CustomExportOptionsPlistContent: os.Getenv("custom_export_options_plist_content"),

		UseLegacyExport:                     os.Getenv("use_legacy_export"),
		LegacyExportProvisioningProfileName: os.Getenv("legacy_export_provisioning_profile_name"),

		DeployDir:  os.Getenv("BITRISE_DEPLOY_DIR"),
		VerboseLog: os.Getenv("verbose_log"),
	}
}

func (configs ConfigsModel) print() {
	log.Infof("Configs:")
	log.Printf("- ArchivePath: %s", configs.ArchivePath)
	log.Printf("- ExportMethod: %s", configs.ExportMethod)
	if configs.ExportMethod == "auto-detect" {
		exportMethods := []exportoptions.Method{exportoptions.MethodAppStore, exportoptions.MethodAdHoc, exportoptions.MethodEnterprise, exportoptions.MethodDevelopment}
		log.Warnf("  Export method: auto-detect is DEPRECATED, use a direct export method %s", exportMethods)
		fmt.Println()
	}
	log.Printf("- UploadBitcode: %s", configs.UploadBitcode)
	log.Printf("- CompileBitcode: %s", configs.CompileBitcode)
	log.Printf("- TeamID: %s", configs.TeamID)

	log.Infof("Experimental Configs:")
	log.Printf("- UseLegacyExport: %s", configs.UseLegacyExport)
	log.Printf("- LegacyExportProvisioningProfileName: %s", configs.LegacyExportProvisioningProfileName)
	log.Printf("- CustomExportOptionsPlistContent:")
	if configs.CustomExportOptionsPlistContent != "" {
		fmt.Println(configs.CustomExportOptionsPlistContent)
	}

	log.Infof("Other Configs:")
	log.Printf("- DeployDir: %s", configs.DeployDir)
	log.Printf("- VerboseLog: %s", configs.VerboseLog)
}

func (configs ConfigsModel) validate() error {
	if configs.ArchivePath == "" {
		return errors.New("no ArchivePath specified")
	}

	if exist, err := pathutil.IsPathExists(configs.ArchivePath); err != nil {
		return fmt.Errorf("failed to check if ArchivePath exist at: %s, error: %s", configs.ArchivePath, err)
	} else if !exist {
		return fmt.Errorf("ArchivePath not exist at: %s", configs.ArchivePath)
	}

	if configs.ExportMethod == "" {
		return errors.New("no ExportMethod specified")
	}
	if configs.UploadBitcode == "" {
		return errors.New("no UploadBitcode specified")
	}
	if configs.CompileBitcode == "" {
		return errors.New("no CompileBitcode specified")
	}

	if configs.UseLegacyExport == "" {
		return errors.New("no UseLegacyExport specified")
	}

	return nil
}

func fail(format string, v ...interface{}) {
	log.Errorf(format, v...)
	os.Exit(1)
}

func findIDEDistrubutionLogsPath(output string) (string, error) {
	pattern := `IDEDistribution: -\[IDEDistributionLogging _createLoggingBundleAtPath:\]: Created bundle at path '(?P<log_path>.*)'`
	re := regexp.MustCompile(pattern)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if match := re.FindStringSubmatch(line); len(match) == 2 {
			return match[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", nil
}

func generateExportOptionsPlist(exportMethodStr, teamID string, uploadBitcode, compileBitcode bool, xcodebuildMajorVersion int64, archive xcarchive.IosArchive) (string, error) {
	log.Printf("Generating export options")

	var exportMethod exportoptions.Method
	exportTeamID := ""
	exportCodeSignIdentity := ""
	exportProfileMapping := map[string]string{}
	exportCodeSignStyle := ""

	if exportMethodStr == "auto-detect" {
		log.Printf("auto-detect export method specified")
		exportMethod = archive.Application.ProvisioningProfile.ExportType

		log.Printf("using the archive profile's export method: %s", exportMethod)
	} else {
		parsedMethod, err := exportoptions.ParseMethod(exportMethodStr)
		if err != nil {
			fail("Failed to parse export options, error: %s", err)
		}
		exportMethod = parsedMethod
		log.Printf("export-method specified: %s", exportMethodStr)
	}

	if xcodebuildMajorVersion >= 9 {
		log.Printf("xcode major version > 9, generating provisioningProfiles node")

		certs, err := certificateutil.InstalledCodesigningCertificateInfos()
		if err != nil {
			fail("Failed to get installed certificates, error: %s", err)
		}
		certs = certificateutil.FilterValidCertificateInfos(certs)

		log.Debugf("Installed certificates:")
		for _, certInfo := range certs {
			log.Debugf(certInfo.String())
		}

		profs, err := profileutil.InstalledProvisioningProfileInfos(profileutil.ProfileTypeIos)
		if err != nil {
			fail("Failed to get installed provisioning profiles, error: %s", err)
		}

		log.Debugf("Installed profiles:")
		for _, profileInfo := range profs {
			log.Debugf(profileInfo.String(certs...))
		}

		bundleIDEntitlementsMap := archive.BundleIDEntitlementsMap()
		bundleIDs := []string{}
		for bundleID := range bundleIDEntitlementsMap {
			bundleIDs = append(bundleIDs, bundleID)
		}

		fmt.Println()
		log.Printf("Target Bundle ID - Entitlements map")
		for bundleID, entitlements := range bundleIDEntitlementsMap {
			entitlementKeys := []string{}
			for key := range entitlements {
				entitlementKeys = append(entitlementKeys, key)
			}
			log.Printf("%s: %s", bundleID, entitlementKeys)
		}

		fmt.Println()
		log.Printf("Resolving CodeSignGroups...")

		codeSignGroups := export.CreateSelectableCodeSignGroups(certs, profs, bundleIDs)
		if len(codeSignGroups) == 0 {
			log.Errorf("Failed to find code signing groups for specified export method (%s)", exportMethod)
		}

		for _, group := range codeSignGroups {
			log.Debugf(group.String())
		}

		filters := []export.SelectableCodeSignGroupFilter{}

		log.Warnf("Filtering CodeSignInfo groups for target capabilities")
		filters = append(filters,
			export.CreateEntitlementsSelectableCodeSignGroupFilter(bundleIDEntitlementsMap))

		log.Warnf("Filtering CodeSignInfo groups for export method")
		filters = append(filters,
			export.CreateExportMethodSelectableCodeSignGroupFilter(exportMethod))

		if teamID != "" {
			log.Warnf("Export TeamID specified: %s, filtering CodeSignInfo groups...", teamID)
			filters = append(filters,
				export.CreateTeamSelectableCodeSignGroupFilter(teamID))
		}

		if !archive.Application.ProvisioningProfile.IsXcodeManaged() {
			log.Warnf("App was signed with NON xcode managed profile when archiving,\n" +
				"only NOT xcode managed profiles are allowed to sign when exporting the archive.\n" +
				"Removing xcode managed CodeSignInfo groups")
			filters = append(filters, export.CreateNotXcodeManagedSelectableCodeSignGroupFilter())
		}

		codeSignGroups = export.FilterSelectableCodeSignGroups(codeSignGroups, filters...)

		defaultProfileURL := os.Getenv("BITRISE_DEFAULT_PROVISION_URL")
		if teamID == "" && defaultProfileURL != "" {
			if defaultProfile, err := utils.GetDefaultProvisioningProfile(); err == nil {
				log.Debugf("\ndefault profile: %v\n", defaultProfile)
				filteredCodeSignGroups := export.FilterSelectableCodeSignGroups(codeSignGroups,
					export.CreateExcludeProfileNameSelectableCodeSignGroupFilter(defaultProfile.Name))
				if len(filteredCodeSignGroups) > 0 {
					codeSignGroups = filteredCodeSignGroups
				}
			}
		}

		iosCodeSignGroups := export.CreateIosCodeSignGroups(codeSignGroups)

		if len(iosCodeSignGroups) > 0 {
			codeSignGroup := export.IosCodeSignGroup{}

			if len(iosCodeSignGroups) >= 1 {
				codeSignGroup = iosCodeSignGroups[0]
			}
			if len(iosCodeSignGroups) > 1 {
				log.Warnf("Multiple code signing groups found! Using the first code signing group")
			}

			exportTeamID = codeSignGroup.Certificate.TeamID
			exportCodeSignIdentity = codeSignGroup.Certificate.CommonName

			for bundleID, profileInfo := range codeSignGroup.BundleIDProfileMap {
				exportProfileMapping[bundleID] = profileInfo.Name

				isXcodeManaged := profileutil.IsXcodeManaged(profileInfo.Name)
				if isXcodeManaged {
					if exportCodeSignStyle != "" && exportCodeSignStyle != "automatic" {
						log.Errorf("Both xcode managed and NON xcode managed profiles in code signing group")
					}
					exportCodeSignStyle = "automatic"
				} else {
					if exportCodeSignStyle != "" && exportCodeSignStyle != "manual" {
						log.Errorf("Both xcode managed and NON xcode managed profiles in code signing group")
					}
					exportCodeSignStyle = "manual"
				}
			}
		} else {
			log.Errorf("Failed to find Codesign Groups")
		}
	}

	var exportOpts exportoptions.ExportOptions
	if exportMethod == exportoptions.MethodAppStore {
		options := exportoptions.NewAppStoreOptions()
		options.UploadBitcode = uploadBitcode

		if xcodebuildMajorVersion >= 9 {
			options.BundleIDProvisioningProfileMapping = exportProfileMapping
			options.SigningCertificate = exportCodeSignIdentity
			options.TeamID = exportTeamID

			if archive.Application.ProvisioningProfile.IsXcodeManaged() && exportCodeSignStyle == "manual" {
				log.Warnf("App was signed with xcode managed profile when archiving,")
				log.Warnf("ipa export uses manual code singing.")
				log.Warnf(`Setting "signingStyle" to "manual"`)

				options.SigningStyle = "manual"
			}
		}

		exportOpts = options
	} else {
		options := exportoptions.NewNonAppStoreOptions(exportMethod)
		options.CompileBitcode = compileBitcode

		if xcodebuildMajorVersion >= 9 {
			options.BundleIDProvisioningProfileMapping = exportProfileMapping
			options.SigningCertificate = exportCodeSignIdentity
			options.TeamID = exportTeamID

			if archive.Application.ProvisioningProfile.IsXcodeManaged() && exportCodeSignStyle == "manual" {
				log.Warnf("App was signed with xcode managed profile when archiving,")
				log.Warnf("ipa export uses manual code singing.")
				log.Warnf(`Setting "signingStyle" to "manual"`)

				options.SigningStyle = "manual"
			}
		}

		exportOpts = options
	}

	return exportOpts.String()
}

func main() {
	configs := createConfigsModelFromEnvs()

	fmt.Println()
	configs.print()

	if err := configs.validate(); err != nil {
		fail("Issue with input: %s", err)
	}

	log.SetEnableDebugLog(configs.VerboseLog == "yes")

	xcodebuildVersion, err := utility.GetXcodeVersion()
	if err != nil {
		fail("Failed to determin xcode version, error: %s", err)
	}
	log.Printf("- xcodebuildVersion: %s (%s)", xcodebuildVersion.Version, xcodebuildVersion.BuildVersion)

	if xcodebuildVersion.MajorVersion >= 9 && configs.UseLegacyExport == "yes" {
		fail("Legacy export method (using '-exportFormat ipa' flag) is not supported from Xcode version 9")
	}

	// Validation CustomExportOptionsPlistContent
	customExportOptionsPlistContent := strings.TrimSpace(configs.CustomExportOptionsPlistContent)
	if customExportOptionsPlistContent != configs.CustomExportOptionsPlistContent {
		fmt.Println()
		log.Warnf("CustomExportOptionsPlistContent is stripped to remove spaces and new lines:")
		log.Printf(customExportOptionsPlistContent)
	}

	archiveExt := filepath.Ext(configs.ArchivePath)
	archiveName := filepath.Base(configs.ArchivePath)
	archiveName = strings.TrimSuffix(archiveName, archiveExt)

	ipaPath := filepath.Join(configs.DeployDir, archiveName+".ipa")
	exportOptionsPath := filepath.Join(configs.DeployDir, "export_options.plist")

	dsymZipPath := filepath.Join(configs.DeployDir, archiveName+".dSYM.zip")
	ideDistributionLogsZipPath := filepath.Join(configs.DeployDir, "xcodebuild.xcdistributionlogs.zip")

	envsToUnset := []string{"GEM_HOME", "GEM_PATH", "RUBYLIB", "RUBYOPT", "BUNDLE_BIN_PATH", "_ORIGINAL_GEM_PATH", "BUNDLE_GEMFILE"}
	for _, key := range envsToUnset {
		if err := os.Unsetenv(key); err != nil {
			fail("Failed to unset (%s), error: %s", key, err)
		}
	}

	archive, err := xcarchive.NewIosArchive(configs.ArchivePath)
	if err != nil {
		fail("Failed to parse archive, error: %s", err)
	}

	mainApplication := archive.Application
	archiveExportMethod := mainApplication.ProvisioningProfile.ExportType
	archiveCodeSignIsXcodeManaged := profileutil.IsXcodeManaged(mainApplication.ProvisioningProfile.Name)

	fmt.Println()
	log.Infof("Archive infos:")
	log.Printf("team: %s (%s)", mainApplication.ProvisioningProfile.TeamName, mainApplication.ProvisioningProfile.TeamID)
	log.Printf("profile: %s (%s)", mainApplication.ProvisioningProfile.Name, mainApplication.ProvisioningProfile.UUID)
	log.Printf("export: %s", archiveExportMethod)
	log.Printf("xcode managed profile: %v", archiveCodeSignIsXcodeManaged)
	fmt.Println()

	if xcodebuildVersion.MajorVersion <= 6 || configs.UseLegacyExport == "yes" {
		log.Infof("Using legacy export method...")

		legacyExportCmd := xcodebuild.NewLegacyExportCommand()
		legacyExportCmd.SetExportFormat("ipa")
		legacyExportCmd.SetArchivePath(configs.ArchivePath)
		legacyExportCmd.SetExportPath(ipaPath)
		legacyExportCmd.SetExportProvisioningProfileName(mainApplication.ProvisioningProfile.Name)

		log.Donef("$ %s", legacyExportCmd.PrintableCmd())
		fmt.Println()

		if err := legacyExportCmd.Run(); err != nil {
			fail("Export failed, error: %s", err)
		}
	} else {
		log.Infof("Exporting with export options...")

		if customExportOptionsPlistContent != "" {
			log.Printf("Custom export options content provided, using it:")
			fmt.Println(customExportOptionsPlistContent)

			if err := fileutil.WriteStringToFile(exportOptionsPath, customExportOptionsPlistContent); err != nil {
				fail("Failed to write export options to file, error: %s", err)
			}
		} else {
			exportOptionsContent, err := generateExportOptionsPlist(configs.ExportMethod, configs.TeamID, configs.UploadBitcode == "true", configs.CompileBitcode == "true", xcodebuildVersion.MajorVersion, archive)
			if err != nil {
				fail("Failed to generate export options, error: %s", err)
			}

			log.Printf("\ngenerated export options content:\n%s", exportOptionsContent)

			if err := fileutil.WriteStringToFile(exportOptionsPath, exportOptionsContent); err != nil {
				fail("Failed to write export options to file, error: %s", err)
			}

			fmt.Println()
		}

		tmpDir, err := pathutil.NormalizedOSTempDirPath("__export__")
		if err != nil {
			fail("Failed to create tmp dir, error: %s", err)
		}

		exportCmd := xcodebuild.NewExportCommand()
		exportCmd.SetArchivePath(configs.ArchivePath)
		exportCmd.SetExportDir(tmpDir)
		exportCmd.SetExportOptionsPlist(exportOptionsPath)

		log.Donef("$ %s", exportCmd.PrintableCmd())
		fmt.Println()

		if xcodebuildOut, err := exportCmd.RunAndReturnOutput(); err != nil {
			// xcdistributionlogs
			if logsDirPth, err := findIDEDistrubutionLogsPath(xcodebuildOut); err != nil {
				log.Warnf("Failed to find xcdistributionlogs, error: %s", err)
			} else if err := utils.ExportOutputDirAsZip(logsDirPth, ideDistributionLogsZipPath, bitriseIDEDistributionLogsPthEnvKey); err != nil {
				log.Warnf("Failed to export %s, error: %s", bitriseIDEDistributionLogsPthEnvKey, err)
			} else {
				log.Warnf(`If you can't find the reason of the error in the log, please check the xcdistributionlogs
The logs directory is stored in $BITRISE_DEPLOY_DIR, and its full path
is available in the $BITRISE_IDEDISTRIBUTION_LOGS_PATH environment variable`)
			}

			fail("Export failed, error: %s", err)
		}

		// Search for ipa
		pattern := filepath.Join(tmpDir, "*.ipa")
		ipas, err := filepath.Glob(pattern)
		if err != nil {
			fail("Failed to collect ipa files, error: %s", err)
		}

		if len(ipas) == 0 {
			fail("No ipa found with pattern: %s", pattern)
		} else if len(ipas) == 1 {
			if err := command.CopyFile(ipas[0], ipaPath); err != nil {
				fail("Failed to copy (%s) -> (%s), error: %s", ipas[0], ipaPath, err)
			}
		} else {
			log.Warnf("More than 1 .ipa file found")

			for _, ipa := range ipas {
				base := filepath.Base(ipa)
				deployPth := filepath.Join(configs.DeployDir, base)

				if err := command.CopyFile(ipa, deployPth); err != nil {
					fail("Failed to copy (%s) -> (%s), error: %s", ipas[0], ipaPath, err)
				}
				ipaPath = ipa
			}
		}

		if err := utils.ExportOutputFile(ipaPath, ipaPath, bitriseIPAPthEnvKey); err != nil {
			fail("Failed to export %s, error: %s", bitriseIPAPthEnvKey, err)
		}

		log.Donef("The ipa path is now available in the Environment Variable: %s (value: %s)", bitriseIPAPthEnvKey, ipaPath)

		appDSYM, _, err := archive.FindDSYMs()
		if err != nil {
			fail("Failed to export dsym, error: %s", err)
		}

		if err := utils.ExportOutputDirAsZip(appDSYM, dsymZipPath, bitriseDSYMPthEnvKey); err != nil {
			fail("Failed to export %s, error: %s", bitriseDSYMPthEnvKey, err)
		}

		log.Donef("The dSYM zip path is now available in the Environment Variable: %s (value: %s)", bitriseDSYMPthEnvKey, dsymZipPath)
	}
}
