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

	DeployDir string
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

		DeployDir: os.Getenv("BITRISE_DEPLOY_DIR"),
	}
}

func (configs ConfigsModel) print() {
	log.Infof("Configs:")
	log.Printf("- ArchivePath: %s", configs.ArchivePath)
	log.Printf("- ExportMethod: %s", configs.ExportMethod)
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

func printCertificateInfo(info certificateutil.CertificateInfoModel) {
	log.Printf(info.CommonName)
	log.Printf("serial: %s", info.Serial)
	log.Printf("team: %s (%s)", info.TeamName, info.TeamID)
	log.Printf("expire: %s", info.EndDate)

	if err := info.CheckValidity(); err != nil {
		log.Errorf("[X] %s", err)
	}
}

func printProfileInfo(info profileutil.ProvisioningProfileInfoModel, installedCertificates []certificateutil.CertificateInfoModel) {
	log.Printf("%s (%s)", info.Name, info.UUID)
	log.Printf("exportType: %s", string(info.ExportType))
	log.Printf("team: %s (%s)", info.TeamName, info.TeamID)
	log.Printf("bundleID: %s", info.BundleID)

	log.Printf("certificates:")
	for _, certificateInfo := range info.DeveloperCertificates {
		log.Printf("- %s", certificateInfo.CommonName)
		log.Printf("  serial: %s", certificateInfo.Serial)
		log.Printf("  teamID: %s", certificateInfo.TeamID)
	}

	if len(info.ProvisionedDevices) > 0 {
		log.Printf("devices:")
		for _, deviceID := range info.ProvisionedDevices {
			log.Printf("- %s", deviceID)
		}
	}

	log.Printf("expire: %s", info.ExpirationDate)

	if !info.HasInstalledCertificate(installedCertificates) {
		log.Errorf("[X] none of the profile's certificates are installed")
	}

	if err := info.CheckValidity(); err != nil {
		log.Errorf("[X] %s", err)
	}

	if info.IsXcodeManaged() {
		log.Warnf("[!] xcode managed profile")
	}
}

func printCodesignGroup(group export.CodeSignGroup) {
	fmt.Printf("development team: %s (%s)\n", group.Certificate.TeamName, group.Certificate.TeamID)
	fmt.Printf("codesign identity: %s [%s]\n", group.Certificate.CommonName, group.Certificate.Serial)
	idx := -1
	for bundleID, profile := range group.BundleIDProfileMap {
		idx++
		if idx == 0 {
			fmt.Printf("provisioning profiles: %s -> %s\n", profile.Name, bundleID)
		} else {
			fmt.Printf("%s%s -> %s\n", strings.Repeat(" ", len("provisioning profiles: ")), profile.Name, bundleID)
		}
	}
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

func main() {
	configs := createConfigsModelFromEnvs()

	fmt.Println()
	configs.print()

	if err := configs.validate(); err != nil {
		fail("Issue with input: %s", err)
	}

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

	archive, err := xcarchive.NewXCArchive(configs.ArchivePath)
	if err != nil {
		fail("Failed to parse archive, error: %s", err)
	}

	mainApplication := archive.Applications.MainApplication
	archiveExportMethod := mainApplication.ProvisioningProfile.ExportType
	archiveCodeSignIsXcodeManaged := profileutil.IsXcodeManaged(mainApplication.ProvisioningProfile.Name)

	fmt.Println()
	log.Infof("Archive infos:")
	log.Printf("team: %s (%s)", mainApplication.ProvisioningProfile.TeamName, mainApplication.ProvisioningProfile.TeamID)
	log.Printf("profile: %s (%s)", mainApplication.ProvisioningProfile.Name, mainApplication.ProvisioningProfile.UUID)
	log.Printf("export: %s", archiveExportMethod)
	log.Printf("xcode managed profile: %v", archiveCodeSignIsXcodeManaged)
	fmt.Println()

	if configs.UseLegacyExport == "yes" {
		log.Infof("Using legacy export method...")

		provisioningProfileName := ""
		if configs.LegacyExportProvisioningProfileName != "" {
			log.Printf("Using provisioning profile: %s", configs.LegacyExportProvisioningProfileName)

			provisioningProfileName = configs.LegacyExportProvisioningProfileName
		} else {
			log.Printf("Using embedded provisioing profile")

			provisioningProfileName = archive.Applications.MainApplication.ProvisioningProfile.Name
			log.Printf("embedded profile name: %s", provisioningProfileName)
		}

		legacyExportCmd := xcodebuild.NewLegacyExportCommand()
		legacyExportCmd.SetExportFormat("ipa")
		legacyExportCmd.SetArchivePath(configs.ArchivePath)
		legacyExportCmd.SetExportPath(ipaPath)
		legacyExportCmd.SetExportProvisioningProfileName(provisioningProfileName)

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
			log.Printf("Generating export options")

			var exportMethod exportoptions.Method
			exportTeamID := ""
			exportCodeSignIdentity := ""
			exportProfileMapping := map[string]string{}
			exportCodeSignStyle := ""

			if configs.ExportMethod == "auto-detect" {
				log.Printf("auto-detect export method specified")
				exportMethod = archiveExportMethod

				log.Printf("using the archive profile's export method: %s", exportMethod)
			} else {
				parsedMethod, err := exportoptions.ParseMethod(configs.ExportMethod)
				if err != nil {
					fail("Failed to parse export options, error: %s", err)
				}
				exportMethod = parsedMethod
				log.Printf("export-method specified: %s", configs.ExportMethod)
			}

			if xcodebuildVersion.MajorVersion >= 9 {
				log.Printf("xcode major version > 9, generating provisioningProfiles node")

				installedCertificates, err := certificateutil.InstalledCodesigningCertificateInfos()
				if err != nil {
					fail("Failed to get installed certificates, error: %s", err)
				}

				fmt.Println()
				log.Printf("Installed Codesign Identities")
				for idx, certificate := range installedCertificates {
					printCertificateInfo(certificate)
					if idx < len(installedCertificates)-1 {
						fmt.Println()
					}
				}

				installedProfiles, err := profileutil.InstalledProvisioningProfileInfos(profileutil.ProfileTypeIos)
				if err != nil {
					fail("Failed to get installed provisioning profiles, error: %s", err)
				}

				fmt.Println()
				log.Printf("Installed Provisioning Profiles")
				for idx, profile := range installedProfiles {
					printProfileInfo(profile, installedCertificates)
					if idx < len(installedProfiles)-1 {
						fmt.Println()
					}
				}

				bundleIDEntitlemnstMap := archive.BundleIDEntitlementsMap()

				fmt.Println()
				log.Printf("Target Bundel ID - Entitlements map")
				idx := -1
				for bundleID, entitlements := range bundleIDEntitlemnstMap {
					idx++
					entitlementKeys := []string{}
					for key := range entitlements {
						entitlementKeys = append(entitlementKeys, key)
					}
					log.Printf("%s: [%s]", bundleID, strings.Join(entitlementKeys, " "))
				}

				codeSignGroups := export.ResolveCodeSignGroups(installedCertificates, installedProfiles, bundleIDEntitlemnstMap)

				fmt.Println()
				log.Printf("Installed codesign settings")
				for idx, group := range codeSignGroups {
					printCodesignGroup(group)
					if idx < len(codeSignGroups)-1 {
						fmt.Println()
					}
				}

				if len(codeSignGroups) == 0 {
					log.Errorf("Failed to find code singing groups for specified export method (%s)", exportMethod)
				}

				// Filter for specified export team
				if len(codeSignGroups) > 0 && configs.TeamID != "" {
					log.Warnf("Filtering CodeSignInfo groups for export method: %s...", exportMethod)

					codeSignGroups = export.FilterCodeSignGroupsForExportMethod(codeSignGroups, exportMethod)

					if len(codeSignGroups) == 0 {
						log.Errorf("Failed to find code singing groups for specified export method (%s)", exportMethod)
					}
				}

				// Handle if archive used NON xcode managed profile
				if len(codeSignGroups) > 0 && !archiveCodeSignIsXcodeManaged {
					log.Warnf("App was signed with NON xcode managed profile when archiving,")
					log.Warnf("only NOT xcode managed profiles are allowed to sign when exporting the archive.")
					log.Warnf("Removing xcode managed CodeSignInfo groups")

					codeSignGroups = export.FilterCodeSignGroupsForNotXcodeManagedProfiles(codeSignGroups)

					if len(codeSignGroups) == 0 {
						log.Errorf("Failed to find code singing groups for specified export method (%s) and WITHOUT xcode managed profiles", exportMethod)
					}
				}

				// Filter for specified export team
				if len(codeSignGroups) > 0 && configs.TeamID != "" {
					log.Warnf("Export TeamID specified: %s, filtering CodeSignInfo groups...", configs.TeamID)

					codeSignGroups = export.FilterCodeSignGroupsForTeam(codeSignGroups, configs.TeamID)

					if len(codeSignGroups) == 0 {
						log.Errorf("Failed to find code singing groups for specified export method (%s) and team (%s)", exportMethod, configs.TeamID)
					}
				}

				// Filter out default code sign files
				if len(codeSignGroups) > 0 && configs.TeamID == "" {
					if defaultProfile, err := utils.GetDefaultProvisioningProfile(); err == nil && defaultProfile.TeamID != "" {
						filteredGroups := []export.CodeSignGroup{}
						for _, group := range codeSignGroups {
							if group.Certificate.TeamID != defaultProfile.TeamID {
								filteredGroups = append(filteredGroups, group)
							}
						}

						if len(filteredGroups) > 0 {
							codeSignGroups = filteredGroups
						}
					}
				}

				fmt.Println()
				log.Printf("Filtered codesign settings")
				for idx, group := range codeSignGroups {
					printCodesignGroup(group)
					if idx < len(codeSignGroups)-1 {
						fmt.Println()
					}
				}

				if len(codeSignGroups) > 0 {
					codeSignGroup := export.CodeSignGroup{}

					if len(codeSignGroups) == 1 {
						codeSignGroup = codeSignGroups[0]
					} else if len(codeSignGroups) > 1 {
						log.Warnf("Multiple code singing groups found")

						codeSignGroup = codeSignGroups[0]
						log.Warnf("Using first group")
					}

					exportTeamID = codeSignGroup.Certificate.TeamID
					exportCodeSignIdentity = codeSignGroup.Certificate.CommonName

					for bundleID, profileInfo := range codeSignGroup.BundleIDProfileMap {
						exportProfileMapping[bundleID] = profileInfo.Name

						isXcodeManaged := profileutil.IsXcodeManaged(profileInfo.Name)
						if isXcodeManaged {
							if exportCodeSignStyle != "" && exportCodeSignStyle != "automatic" {
								log.Errorf("Both xcode managed and NON xcode managed profiles in code singing group")
							}
							exportCodeSignStyle = "automatic"
						} else {
							if exportCodeSignStyle != "" && exportCodeSignStyle != "manual" {
								log.Errorf("Both xcode managed and NON xcode managed profiles in code singing group")
							}
							exportCodeSignStyle = "manual"
						}
					}
				}
			}

			var exportOpts exportoptions.ExportOptions
			if exportMethod == exportoptions.MethodAppStore {
				options := exportoptions.NewAppStoreOptions()
				options.UploadBitcode = (configs.UploadBitcode == "yes")

				if xcodebuildVersion.MajorVersion >= 9 {
					options.BundleIDProvisioningProfileMapping = exportProfileMapping
					options.SigningCertificate = exportCodeSignIdentity
					options.TeamID = exportTeamID

					if archiveCodeSignIsXcodeManaged && exportCodeSignStyle == "manual" {
						log.Warnf("App was signed with xcode managed profile when archiving,")
						log.Warnf("ipa export uses manual code singing.")
						log.Warnf(`Setting "signingStyle" to "manual"`)

						options.SigningStyle = "manual"
					}
				}

				exportOpts = options
			} else {
				options := exportoptions.NewNonAppStoreOptions(exportMethod)
				options.CompileBitcode = (configs.CompileBitcode == "yes")

				if xcodebuildVersion.MajorVersion >= 9 {
					options.BundleIDProvisioningProfileMapping = exportProfileMapping
					options.SigningCertificate = exportCodeSignIdentity
					options.TeamID = exportTeamID

					if archiveCodeSignIsXcodeManaged && exportCodeSignStyle == "manual" {
						log.Warnf("App was signed with xcode managed profile when archiving,")
						log.Warnf("ipa export uses manual code singing.")
						log.Warnf(`Setting "signingStyle" to "manual"`)

						options.SigningStyle = "manual"
					}
				}

				exportOpts = options
			}

			fmt.Println()
			log.Printf("generated export options content:")
			fmt.Println()
			fmt.Println(exportOpts.String())

			if err = exportOpts.WriteToFile(exportOptionsPath); err != nil {
				fail("Failed to write export options to file, error: %s", err)
			}
		}

		fmt.Println()

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
