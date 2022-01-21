package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-xcode/certificateutil"
	"github.com/bitrise-io/go-xcode/export"
	"github.com/bitrise-io/go-xcode/exportoptions"
	"github.com/bitrise-io/go-xcode/profileutil"
	"github.com/bitrise-io/go-xcode/v2/xcarchive"
)

const (
	// ExportProductApp ...
	ExportProductApp ExportProduct = "app"
	// ExportProductAppClip ...
	ExportProductAppClip ExportProduct = "app-clip"
)

// ExportProduct ...
type ExportProduct string

// ParseExportProduct ...
func ParseExportProduct(product string) (ExportProduct, error) {
	switch product {
	case "app":
		return ExportProductApp, nil
	case "app-clip":
		return ExportProductAppClip, nil
	default:
		return "", fmt.Errorf("unkown method (%s)", product)
	}
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

func generateExportOptionsPlist(exportProduct ExportProduct, exportMethodStr, teamID string, uploadBitcode, compileBitcode bool, xcodebuildMajorVersion int64, archive xcarchive.IosArchive, manageVersionAndBuildNumber bool) (string, error) {
	log.Printf("Generating export options")

	var productBundleID string
	var exportMethod exportoptions.Method
	exportTeamID := ""
	exportCodeSignIdentity := ""
	exportProfileMapping := map[string]string{}
	exportCodeSignStyle := ""

	switch exportProduct {
	case ExportProductApp:
		productBundleID = archive.Application.BundleIdentifier()
	case ExportProductAppClip:
		productBundleID = archive.Application.ClipApplication.BundleIdentifier()
	}

	parsedMethod, err := exportoptions.ParseMethod(exportMethodStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse export options, error: %s", err)
	}
	exportMethod = parsedMethod
	log.Printf("export-method specified: %s", exportMethodStr)

	if xcodebuildMajorVersion >= 9 {
		log.Printf("xcode major version > 9, generating provisioningProfiles node")

		fmt.Println()
		log.Printf("Target Bundle ID - Entitlements map")
		var bundleIDs []string
		for bundleID, entitlements := range archive.BundleIDEntitlementsMap() {
			bundleIDs = append(bundleIDs, bundleID)

			entitlementKeys := []string{}
			for key := range entitlements {
				entitlementKeys = append(entitlementKeys, key)
			}
			log.Printf("%s: %s", bundleID, entitlementKeys)
		}

		fmt.Println()
		log.Printf("Resolving CodeSignGroups...")

		certs, err := certificateutil.InstalledCodesigningCertificateInfos()
		if err != nil {
			return "", fmt.Errorf("failed to get installed certificates, error: %s", err)
		}
		certs = certificateutil.FilterValidCertificateInfos(certs).ValidCertificates

		log.Debugf("Installed certificates:")
		for _, certInfo := range certs {
			log.Debugf(certInfo.String())
		}

		profs, err := profileutil.InstalledProvisioningProfileInfos(profileutil.ProfileTypeIos)
		if err != nil {
			return "", fmt.Errorf("failed to get installed provisioning profiles, error: %s", err)
		}

		log.Debugf("Installed profiles:")
		for _, profileInfo := range profs {
			log.Debugf(profileInfo.String(certs...))
		}

		log.Printf("Resolving CodeSignGroups...")
		codeSignGroups := export.CreateSelectableCodeSignGroups(certs, profs, bundleIDs)
		if len(codeSignGroups) == 0 {
			log.Errorf("Failed to find code signing groups for specified export method (%s)", exportMethod)
		}

		log.Debugf("\nGroups:")
		for _, group := range codeSignGroups {
			log.Debugf(group.String())
		}

		bundleIDEntitlementsMap := archive.BundleIDEntitlementsMap()
		for bundleID := range bundleIDEntitlementsMap {
			bundleIDs = append(bundleIDs, bundleID)
		}

		if len(bundleIDEntitlementsMap) > 0 {
			log.Warnf("Filtering CodeSignInfo groups for target capabilities")

			codeSignGroups = export.FilterSelectableCodeSignGroups(codeSignGroups, export.CreateEntitlementsSelectableCodeSignGroupFilter(bundleIDEntitlementsMap))

			log.Debugf("\nGroups after filtering for target capabilities:")
			for _, group := range codeSignGroups {
				log.Debugf(group.String())
			}
		}

		log.Warnf("Filtering CodeSignInfo groups for export method")

		codeSignGroups = export.FilterSelectableCodeSignGroups(codeSignGroups, export.CreateExportMethodSelectableCodeSignGroupFilter(exportMethod))

		log.Debugf("\nGroups after filtering for export method:")
		for _, group := range codeSignGroups {
			log.Debugf(group.String())
		}

		if teamID != "" {
			log.Warnf("Export TeamID specified: %s, filtering CodeSignInfo groups...", teamID)

			codeSignGroups = export.FilterSelectableCodeSignGroups(codeSignGroups, export.CreateTeamSelectableCodeSignGroupFilter(teamID))

			log.Debugf("\nGroups after filtering for team ID:")
			for _, group := range codeSignGroups {
				log.Debugf(group.String())
			}
		}

		if !archive.Application.ProvisioningProfile.IsXcodeManaged() {
			log.Warnf("App was signed with NON xcode managed profile when archiving,\n" +
				"only NOT xcode managed profiles are allowed to sign when exporting the archive.\n" +
				"Removing xcode managed CodeSignInfo groups")

			codeSignGroups = export.FilterSelectableCodeSignGroups(codeSignGroups, export.CreateNotXcodeManagedSelectableCodeSignGroupFilter())

			log.Debugf("\nGroups after filtering for NOT Xcode managed profiles:")
			for _, group := range codeSignGroups {
				log.Debugf(group.String())
			}
		}

		defaultProfileURL := os.Getenv("BITRISE_DEFAULT_PROVISION_URL")
		if teamID == "" && defaultProfileURL != "" {
			if defaultProfile, err := getDefaultProvisioningProfile(); err == nil {
				log.Debugf("\ndefault profile: %v\n", defaultProfile)
				filteredCodeSignGroups := export.FilterSelectableCodeSignGroups(codeSignGroups,
					export.CreateExcludeProfileNameSelectableCodeSignGroupFilter(defaultProfile.Name))
				if len(filteredCodeSignGroups) > 0 {
					codeSignGroups = filteredCodeSignGroups

					log.Debugf("\nGroups after removing default profile:")
					for _, group := range codeSignGroups {
						log.Debugf(group.String())
					}
				}
			}
		}

		var iosCodeSignGroups []export.IosCodeSignGroup

		for _, selectable := range codeSignGroups {
			bundleIDProfileMap := map[string]profileutil.ProvisioningProfileInfoModel{}
			for bundleID, profiles := range selectable.BundleIDProfilesMap {
				if len(profiles) > 0 {
					bundleIDProfileMap[bundleID] = profiles[0]
				} else {
					log.Warnf("No profile available to sign (%s) target!", bundleID)
				}
			}

			iosCodeSignGroups = append(iosCodeSignGroups, *export.NewIOSGroup(selectable.Certificate, bundleIDProfileMap))
		}

		log.Debugf("\nFiltered groups:")
		for i, group := range iosCodeSignGroups {
			log.Debugf("Group #%d:", i)
			for bundleID, profile := range group.BundleIDProfileMap() {
				log.Debugf(" - %s: %s (%s)", bundleID, profile.Name, profile.UUID)
			}
		}

		if len(iosCodeSignGroups) > 0 {
			codeSignGroup := export.IosCodeSignGroup{}

			if len(iosCodeSignGroups) >= 1 {
				codeSignGroup = iosCodeSignGroups[0]
			}
			if len(iosCodeSignGroups) > 1 {
				log.Warnf("Multiple code signing groups found! Using the first code signing group")
			}

			exportTeamID = codeSignGroup.Certificate().TeamID
			exportCodeSignIdentity = codeSignGroup.Certificate().CommonName

			for bundleID, profileInfo := range codeSignGroup.BundleIDProfileMap() {
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

		if xcodebuildMajorVersion >= 13 {
			log.Debugf("Setting flag for managing app version and build number")

			options.ManageAppVersion = manageVersionAndBuildNumber
		}

		exportOpts = options
	} else {
		options := exportoptions.NewNonAppStoreOptions(exportMethod)
		options.CompileBitcode = compileBitcode

		if xcodebuildMajorVersion >= 12 {
			options.DistributionBundleIdentifier = productBundleID
		}

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

func getDefaultProvisioningProfile() (profileutil.ProvisioningProfileInfoModel, error) {
	defaultProfileURL := os.Getenv("BITRISE_DEFAULT_PROVISION_URL")
	if defaultProfileURL == "" {
		return profileutil.ProvisioningProfileInfoModel{}, nil
	}

	tmpDir, err := pathutil.NormalizedOSTempDirPath("tmp_default_profile")
	if err != nil {
		return profileutil.ProvisioningProfileInfoModel{}, err
	}

	tmpDst := filepath.Join(tmpDir, "default.mobileprovision")
	tmpDstFile, err := os.Create(tmpDst)
	if err != nil {
		return profileutil.ProvisioningProfileInfoModel{}, err
	}
	defer func() {
		if err := tmpDstFile.Close(); err != nil {
			log.Errorf("Failed to close file (%s), error: %s", tmpDst, err)
		}
	}()

	response, err := http.Get(defaultProfileURL)
	if err != nil {
		return profileutil.ProvisioningProfileInfoModel{}, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Errorf("Failed to close response body, error: %s", err)
		}
	}()

	if _, err := io.Copy(tmpDstFile, response.Body); err != nil {
		return profileutil.ProvisioningProfileInfoModel{}, err
	}

	defaultProfile, err := profileutil.NewProvisioningProfileInfoFromFile(tmpDst)
	if err != nil {
		return profileutil.ProvisioningProfileInfoModel{}, err
	}

	return defaultProfile, nil
}
