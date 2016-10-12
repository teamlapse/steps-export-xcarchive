package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/go-utils/cmdex"
	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-tools/go-xcode/exportoptions"
	"github.com/bitrise-tools/go-xcode/provisioningprofile"
	"github.com/bitrise-tools/go-xcode/xcarchive"
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
	log.Info("Configs:")
	log.Detail("- ArchivePath: %s", configs.ArchivePath)
	log.Detail("- ExportMethod: %s", configs.ExportMethod)
	log.Detail("- UploadBitcode: %s", configs.UploadBitcode)
	log.Detail("- CompileBitcode: %s", configs.CompileBitcode)
	log.Detail("- TeamID: %s", configs.TeamID)
	log.Detail("- CustomExportOptionsPlistContent:")
	fmt.Println(configs.CustomExportOptionsPlistContent)
	log.Detail("- UseLegacyExport: %s", configs.UseLegacyExport)
	log.Detail("- LegacyExportProvisioningProfileName: %s", configs.LegacyExportProvisioningProfileName)
	fmt.Println()
	log.Detail("- DeployDir: %s", configs.DeployDir)
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

func exportEnvironmentWithEnvman(keyStr, valueStr string) error {
	cmd := cmdex.NewCommand("envman", "add", "--key", keyStr)
	cmd.SetStdin(strings.NewReader(valueStr))
	return cmd.Run()
}

func exportZipedArtifactDir(pth, deployDir, envKey string) (string, error) {
	parentDir := filepath.Dir(pth)
	dirName := filepath.Base(pth)
	deployPth := filepath.Join(deployDir, dirName+".zip")
	cmd := cmdex.NewCommand("/usr/bin/zip", "-rTy", deployPth, dirName)
	cmd.SetDir(parentDir)
	out, err := cmd.RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Failed to zip dir: %s, output: %s, error: %s", pth, out, err)
	}

	if err := exportEnvironmentWithEnvman(envKey, deployPth); err != nil {
		return "", fmt.Errorf("Failed to export artifact path (%s) into (%s)", deployPth, envKey)
	}

	return deployPth, nil
}

func exportArtifactFile(pth, deployDir, envKey string) (string, error) {
	base := filepath.Base(pth)
	deployPth := filepath.Join(deployDir, base)

	if err := cmdex.CopyFile(pth, deployPth); err != nil {
		return "", fmt.Errorf("Failed to move artifact (%s) to (%s)", pth, deployPth)
	}

	if err := exportEnvironmentWithEnvman(envKey, deployPth); err != nil {
		return "", fmt.Errorf("Failed to export artifact path (%s) into (%s)", deployPth, envKey)
	}

	return deployPth, nil
}

func main() {
	configs := createConfigsModelFromEnvs()

	fmt.Println()
	configs.print()

	if err := configs.validate(); err != nil {
		fmt.Println()
		log.Error("Issue with input: %s", err)
		fmt.Println()

		os.Exit(1)
	}
	fmt.Println()

	callback := func(printableCommand string) {
		log.Done("$ %s", printableCommand)
		fmt.Println()
	}

	ipaPth := ""

	if configs.UseLegacyExport == "yes" {
		log.Info("Using legacy export method...")

		provisioningProfileName := ""
		if configs.LegacyExportProvisioningProfileName != "" {
			log.Detail("Using provisioning profile: %s", configs.LegacyExportProvisioningProfileName)

			provisioningProfileName = configs.LegacyExportProvisioningProfileName
		} else {
			log.Detail("Using embedded provisioing profile")

			embeddedProfilePth, err := xcarchive.EmbeddedMobileProvisionPth(configs.ArchivePath)
			if err != nil {
				log.Error("Failed to get embedded profile path, error: %s", err)
				os.Exit(1)
			}

			provProfile, err := provisioningprofile.NewFromFile(embeddedProfilePth)
			if err != nil {
				log.Error("Failed to create provisioning profile model, error: %s", err)
				os.Exit(1)
			}

			if provProfile.Name == nil {
				log.Error("Profile name empty")
				os.Exit(1)
			}

			log.Detail("embedded profile name: %s", *provProfile.Name)
			provisioningProfileName = *provProfile.Name
		}

		output, err := xcarchive.LegacyExport(configs.ArchivePath, provisioningProfileName, xcarchive.ExportFormatIPA, callback)
		if err != nil {
			log.Error("Export failed, error: %s", err)
			os.Exit(1)
		}

		ipaPth = output
	} else {
		log.Info("Exporting with export options...")

		exportOptionsPth := ""

		if configs.CustomExportOptionsPlistContent != "" {
			log.Detail("Custom export options content provided:")
			fmt.Println(configs.CustomExportOptionsPlistContent)

			tmpDir, err := pathutil.NormalizedOSTempDirPath("export")
			if err != nil {
				log.Error("Failed to create tmp dir, error: %s", err)
				os.Exit(1)
			}
			exportOptionsPth = filepath.Join(tmpDir, "export-options.plist")

			if err := fileutil.WriteStringToFile(exportOptionsPth, configs.CustomExportOptionsPlistContent); err != nil {
				log.Error("Failed to write export options to file, error: %s", err)
				os.Exit(1)
			}
		} else {
			log.Detail("Generating export options")

			var exportOpts exportoptions.ExportOptions

			if configs.ExportMethod == "auto-detect" {
				log.Detail("creating default export options based on embedded profile")

				embeddedProfilePth, err := xcarchive.EmbeddedMobileProvisionPth(configs.ArchivePath)
				if err != nil {
					log.Error("Failed to get embedded profile path, error: %s", err)
					os.Exit(1)
				}

				provProfile, err := provisioningprofile.NewFromFile(embeddedProfilePth)
				if err != nil {
					log.Error("Failed to create provisioning profile model, error: %s", err)
					os.Exit(1)
				}

				if provProfile.Name != nil {
					log.Detail("embedded profile name: %s", *provProfile.Name)
				}

				options, err := xcarchive.DefaultExportOptions(provProfile)
				if err != nil {
					log.Error("Failed to create default export options, error: %s", err)
					os.Exit(1)
				}

				exportOpts = options
			} else {
				method, err := exportoptions.ParseMethod(configs.ExportMethod)
				if err != nil {
					log.Error("Failed to parse export options, error: %s", err)
					os.Exit(1)
				}

				if method == exportoptions.MethodAppStore {
					options := exportoptions.NewAppStoreOptions()
					options.UploadBitcode = (configs.UploadBitcode == "yes")
					options.TeamID = configs.TeamID

					exportOpts = options
				} else {
					options := exportoptions.NewNonAppStoreOptions(method)
					options.CompileBitcode = (configs.CompileBitcode == "yes")
					options.TeamID = configs.TeamID

					exportOpts = options
				}
			}

			log.Detail("generated export options content:")
			fmt.Println(exportOpts.String())

			var err error
			exportOptionsPth, err = exportOpts.WriteToTmpFile()
			if err != nil {
				log.Error("Failed to write export options to file, error: %s", err)
				os.Exit(1)
			}
		}

		output, err := xcarchive.Export(configs.ArchivePath, exportOptionsPth, callback)
		if err != nil {
			log.Error("Export failed, error: %s", err)
			os.Exit(1)
		}

		ipaPth = output
	}

	{
		envKey := "BITRISE_IPA_PATH"
		pth, err := exportArtifactFile(ipaPth, configs.DeployDir, envKey)
		if err != nil {
			log.Error("Failed to export ipa, error: %s", err)
			os.Exit(1)
		}
		log.Done("ipa path (%s) is available in (%s) environment variable", pth, envKey)
	}

	appDSYM, _, err := xcarchive.ExportDSYMs(configs.ArchivePath)
	if err != nil {
		log.Error("Failed to export dsym, error: %s", err)
		os.Exit(1)
	}

	{
		envKey := "BITRISE_DSYM_PATH"
		pth, err := exportZipedArtifactDir(appDSYM, configs.DeployDir, envKey)
		if err != nil {
			log.Error("Failed to export dsym, error: %s", err)
			os.Exit(1)
		}
		log.Done("dsym path (%s) is available in (%s) environment variable", pth, envKey)
	}
}
