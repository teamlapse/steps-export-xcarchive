package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-tools/go-xcode/xcarchive"
)

// ArchiveType ...
type ArchiveType string

const (
	// ArchiveTypeIOS ...
	ArchiveTypeIOS ArchiveType = "ios"
	// ArchiveTypeMacOS ...
	ArchiveTypeMacOS ArchiveType = "mac"
	// ArchiveTypeTvOS ...
	ArchiveTypeTvOS ArchiveType = "tv"
)

// ConfigsModel ...
type ConfigsModel struct {
	IOSArchivePath    string
	TvOSArchivePath   string
	MacOSSArchivePath string

	ExportMethod                    string
	UploadBitcode                   string
	CompileBitcode                  string
	TeamID                          string
	RawExportFormat                 string
	CustomExportOptionsPlistContent string

	UseDeprecatedExport                     string
	DeprecatedExportProvisioningProfileName string

	DeployDir string

	ArchivePath  string
	ArchiveType  ArchiveType
	ExportFormat xcarchive.ExportFormat
}

func createConfigsModelFromEnvs() ConfigsModel {
	return ConfigsModel{
		IOSArchivePath:    os.Getenv("ios_archive_path"),
		TvOSArchivePath:   os.Getenv("tvos_archive_path"),
		MacOSSArchivePath: os.Getenv("macos_archive_path"),

		ExportMethod:                    os.Getenv("export_method"),
		UploadBitcode:                   os.Getenv("upload_bitcode"),
		CompileBitcode:                  os.Getenv("compile_bitcode"),
		TeamID:                          os.Getenv("team_id"),
		RawExportFormat:                 os.Getenv("export_format"),
		CustomExportOptionsPlistContent: os.Getenv("custom_export_options_plist_content"),

		UseDeprecatedExport:                     os.Getenv("use_deprecated_export"),
		DeprecatedExportProvisioningProfileName: os.Getenv("deprecated_export_provisioning_profile_name"),

		DeployDir: os.Getenv("BITRISE_DEPLOY_DIR"),
	}
}

func (configs ConfigsModel) print() {
	log.Info("Archive Path:")
	log.Detail("- IOSArchivePath: %s", configs.IOSArchivePath)
	log.Detail("- TvOSArchivePath: %s", configs.TvOSArchivePath)
	log.Detail("- MacOSSArchivePath: %s", configs.MacOSSArchivePath)
	fmt.Println()

	log.Info("Export Configs:")
	log.Detail("- UseDeprecatedExport: %s", configs.UseDeprecatedExport)

	if configs.CustomExportOptionsPlistContent != "" {
		log.Warn("Ignoring the following options because custom_export_options_plist_content provided:")
	}
	log.Detail("- ExportMethod: %s", configs.ExportMethod)
	log.Detail("- UploadBitcode: %s", configs.UploadBitcode)
	log.Detail("- CompileBitcode: %s", configs.CompileBitcode)
	log.Detail("- TeamID: %s", configs.TeamID)
	if configs.CustomExportOptionsPlistContent != "" {
		log.Warn("----------")
	}
	log.Detail("- CustomExportOptionsPlistContent:")
	fmt.Println(configs.CustomExportOptionsPlistContent)
	fmt.Println()

	log.Detail("- DeployDir: %s", configs.DeployDir)
}

func (configs ConfigsModel) validate() error {
	if configs.IOSArchivePath == "" && configs.TvOSArchivePath == "" && configs.MacOSSArchivePath == "" {
		return errors.New("no iOS, tvOS or macOS archive specified")
	}

	if configs.IOSArchivePath != "" {
		if configs.TvOSArchivePath != "" {
			return errors.New("both iOS and tvOS archive specified, only 1 archive is allowed")
		}
		if configs.MacOSSArchivePath != "" {
			return errors.New("both iOS and macOS archive specified, only 1 archive is allowed")
		}

		if exist, err := pathutil.IsPathExists(configs.IOSArchivePath); err != nil {
			return fmt.Errorf("failed to check if archive exist at: %s, error: %s", configs.IOSArchivePath, err)
		} else if !exist {
			return fmt.Errorf("archive not exist at: %s", configs.IOSArchivePath)
		}
	}

	if configs.TvOSArchivePath != "" {
		if configs.IOSArchivePath != "" {
			return errors.New("both tvOS and iOS archive specified, only 1 archive is allowed")
		}
		if configs.MacOSSArchivePath != "" {
			return errors.New("both tvOS and macOS archive specified, only 1 archive is allowed")
		}

		if exist, err := pathutil.IsPathExists(configs.TvOSArchivePath); err != nil {
			return fmt.Errorf("failed to check if archive exist at: %s, error: %s", configs.TvOSArchivePath, err)
		} else if !exist {
			return fmt.Errorf("archive not exist at: %s", configs.TvOSArchivePath)
		}
	}

	if configs.MacOSSArchivePath != "" {
		if configs.IOSArchivePath != "" {
			return errors.New("Both macOS and iOS archive specified, only 1 archive is allowed!")
		}
		if configs.TvOSArchivePath != "" {
			return errors.New("Both macOS and tvOS archive specified, only 1 archive is allowed!")
		}

		if exist, err := pathutil.IsPathExists(configs.MacOSSArchivePath); err != nil {
			return fmt.Errorf("failed to check if archive exist at: %s, error: %s", configs.MacOSSArchivePath, err)
		} else if !exist {
			return fmt.Errorf("archive not exist at: %s", configs.MacOSSArchivePath)
		}
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

	return nil
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

	exportFormat, err := xcarchive.ParseExportFormat(configs.RawExportFormat)
	if err != nil {
		log.Error("Failed to parse export format, error: %s", err)
		os.Exit(1)
	}
	configs.ExportFormat = exportFormat

	if configs.IOSArchivePath != "" {
		configs.ArchivePath = configs.IOSArchivePath
		configs.ArchiveType = ArchiveTypeIOS
		configs.ExportFormat = xcarchive.ExportFormatIPA
	} else if configs.TvOSArchivePath != "" {
		configs.ArchivePath = configs.TvOSArchivePath
		configs.ArchiveType = ArchiveTypeTvOS
		configs.ExportFormat = xcarchive.ExportFormatIPA
	} else if configs.MacOSSArchivePath != "" {
		configs.ArchivePath = configs.MacOSSArchivePath
		configs.ArchiveType = ArchiveTypeMacOS
		configs.ExportFormat = xcarchive.ExportFormatAPP
	}

	callback := func(printableCommand string) {
		log.Done("$ %s", printableCommand)
		fmt.Println()
	}

	if configs.UseDeprecatedExport == "yes" {
		log.Info("Using legacy export method...")

		provisioningProfileName := ""
		if configs.DeprecatedExportProvisioningProfileName != "" {
			log.Detail("Using provisioning profile: %s", configs.DeprecatedExportProvisioningProfileName)
			provisioningProfileName = configs.DeprecatedExportProvisioningProfileName
		} else if configs.ArchiveType == ArchiveTypeMacOS {
			log.Detail("No provisining profile specified, let xcodebuild to grab one...")
		} else {
			log.Detail("Using embedded provisioing profile")
			profileName, err := xcarchive.EmbeddedProfileName(configs.ArchivePath)
			if err != nil {
				log.Error("Failed to find embedded profile, error: %s", err)
				os.Exit(1)
			}
			provisioningProfileName = profileName
		}

		output, err := xcarchive.LegacyExport(configs.ArchivePath, provisioningProfileName, configs.ExportFormat, callback)
		if err != nil {
			log.Error("Export failed, error: %s", err)
			os.Exit(1)
		}

		log.Done("Output: %s", output)
	} else {
		log.Info("Exporting with export options...")

		if configs.CustomExportOptionsPlistContent != "" {
			log.Detail("Custom export options content provided:")
			fmt.Println(configs.CustomExportOptionsPlistContent)

			tmpDir, err := pathutil.NormalizedOSTempDirPath("export")
			if err != nil {
				log.Error("Failed to create tmp dir, error: %s", err)
				os.Exit(1)
			}
			exportOptionsPth := filepath.Join(tmpDir, "export-options.plist")

			if err := fileutil.WriteStringToFile(exportOptionsPth, configs.CustomExportOptionsPlistContent); err != nil {
				log.Error("Failed to write export options to file, error: %s", err)
				os.Exit(1)
			}

			output, err := xcarchive.Export(configs.ArchivePath, exportOptionsPth, configs.ExportFormat, callback)
			if err != nil {
				log.Error("Export failed, error: %s", err)
				os.Exit(1)
			}
			log.Done("Output: %s", output)
		} else {
			log.Detail("Generating export options")

		}
	}

	// //
	// // Prepare export options
	// exportOptionsPth := ""

	// if configs.CustomExportOptionsPlistContent != "" {
	// 	if configs.IOSArchivePath != "" {
	// tmpDir, err := pathutil.NormalizedOSTempDirPath("export")
	// if err != nil {
	// 	log.Error("Failed to create tmp dir, error: %s", err)
	// 	os.Exit(1)
	// }
	// exportOptionsPth = filepath.Join(tmpDir, "export-options.plist")

	// if err := fileutil.WriteStringToFile(exportOptionsPth, configs.CustomExportOptionsPlistContent); err != nil {
	// 	log.Error("Failed to write export options to file, error: %s", err)
	// }
	// 	}
	// } else {
	// 	var err error
	// 	var options exportoptions.ExportOptions

	// 	if configs.ExportMethod == "auto-detect" {
	// 		options, err = xcarchive.DefaultExportOptions(configs.ArchivePath)
	// 		if err != nil {
	// 			log.Error("Failed to generate default export options, errror: %s", err)
	// 			os.Exit(1)
	// 		}
	// 	} else {
	// 		method, err := exportoptions.ParseMethod(configs.ExportMethod)
	// 		if err != nil {
	// 			log.Error("Failed to parse export method (%s), err: %s", configs.ExportMethod, err)
	// 		}

	// 		if method == exportoptions.MethodAppStore {
	// 			options := exportoptions.NewAppStoreOptions()
	// 			options.UploadBitcode = (configs.UploadBitcode == "yes")
	// 			options.TeamID = configs.TeamID
	// 		} else {
	// 			options := exportoptions.NewNonAppStoreOptions(method)
	// 			options.CompileBitcode = (configs.CompileBitcode == "yes")
	// 			options.TeamID = configs.TeamID
	// 		}
	// 	}

	// 	exportOptionsPth, err = options.WriteToTmpFile()
	// 	if err != nil {
	// 		log.Error("Failed to write export options to file, error: %s", err)
	// 	}
	// }
	// // ---

	// //
	// // Export archive
	// archivePth := configs.ArchivePath
	// outputEnvKey := ""
	// if configs.IOSArchivePath != "" {
	// 	outputEnvKey = "BITRISE_IOS_IPA_PTH"
	// } else if configs.TvOSArchivePath != "" {
	// 	outputEnvKey = "BITRISE_TVOS_IPA_PTH"
	// } else if configs.MacOSSArchivePath != "" {
	// 	if configs.UseDeprecatedExport == "yes" {
	// 		outputEnvKey = "BITRISE_MACOS_APP_PTH"
	// 	} else {
	// 		method, err := exportoptions.ParseMethod(configs.ExportMethod)
	// 		if err != nil {
	// 			log.Error("Failed to parse export method (%s), err: %s", configs.ExportMethod, err)
	// 		}

	// 		if method == exportoptions.MethodAppStore {
	// 			outputEnvKey = "BITRISE_MACOS_PKG_PTH"
	// 		} else {
	// 			outputEnvKey = "BITRISE_MACOS_APP_PTH"
	// 		}
	// 	}
	// }

	// exportFormat := xcarchive.ExportFormatIPA
	// if configs.MacOSSArchivePath != "" {
	// 	exportFormat = xcarchive.ExportFormatAPP
	// }

	// callback := func(printableCommand string) {
	// 	log.Done("$ %s", printableCommand)
	// 	fmt.Println()
	// }

	// outputPth := ""

	// if configs.UseDeprecatedExport == "yes" {
	// 	profileName, err := xcarchive.EmbeddedProfileName(archivePth)
	// 	if err != nil {
	// 		log.Error("Failed to get embedded profile name, error: %s", err)
	// 		os.Exit(1)
	// 	}

	// 	outputPth, err = xcarchive.LegacyExport(archivePth, profileName, exportFormat, callback)
	// 	if err != nil {
	// 		log.Error("Export failed, error: %s", err)
	// 		os.Exit(1)
	// 	}
	// } else if configs.MacOSSArchivePath != "" {
	// 	var err error
	// 	outputPth, err = xcarchive.ExportAPP(archivePth, exportOptionsPth, callback)
	// 	if err != nil {
	// 		log.Error("Export failed, error: %s", err)
	// 		os.Exit(1)
	// 	}
	// } else {
	// 	var err error
	// 	outputPth, err = xcarchive.ExportIPA(archivePth, exportOptionsPth, callback)
	// 	if err != nil {
	// 		log.Error("Export failed, error: %s", err)
	// 		os.Exit(1)
	// 	}
	// }

	// log.Done("output path (%s) is available in (%s) environment variable", outputPth, outputEnvKey)
	// // ---
}
