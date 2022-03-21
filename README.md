# Export iOS and tvOS Xcode archive

[![Step changelog](https://shields.io/github/v/release/bitrise-steplib/steps-export-xcarchive?include_prereleases&label=changelog&color=blueviolet)](https://github.com/bitrise-steplib/steps-export-xcarchive/releases)

Export iOS and tvOS IPA from an existing Xcode archive

<details>
<summary>Description</summary>

Exports an IPA from an existing iOS and tvOS `.xcarchive` file. You can add multiple **Export iOS and tvOS Xcode archive** Steps to your Workflows to create multiple different signed .ipa files.
The Step also logs you into your Apple Developer account based on the [Apple service connection you provide on Bitrise](https://devcenter.bitrise.io/en/accounts/connecting-to-services/apple-services-connection.html) and downloads any provisioning profiles needed for your project based on the **Distribution method**.

### Configuring the Step
Before you start:
- Make sure you have connected your [Apple Service account to Bitrise](https://devcenter.bitrise.io/en/accounts/connecting-to-services/apple-services-connection.html).
Alternatively, you can upload certificates and profiles to Bitrise manually, then use the Certificate and Profile installer step before Xcode Archive
- Make sure certificates are uploaded to Bitrise's **Code Signing** tab. The right provisioning profiles are automatically downloaded from Apple as part of the automatic code signing process.

To configure the Step:
1. **Archive Path**: Specifies the archive that should be exported. The input value sets xcodebuild's `-archivePath` option.
2. **Select a product to distribute**: Decide if an App or an App Clip IPA should be exported.
3. **Distribution method**: Describes how Xcode should export the archive: development, app-store, ad-hoc, or enterprise.

Under **Automatic code signing**:
1. **Automatic code signing method**: Select the Apple service connection you want to use for code signing. Available options: `off` if you don't do automatic code signing, `api-key` [if you use API key authorization](https://devcenter.bitrise.io/en/accounts/connecting-to-services/connecting-to-an-apple-service-with-api-key.html), and `apple-id` [if you use Apple ID authorization](https://devcenter.bitrise.io/en/accounts/connecting-to-services/connecting-to-an-apple-service-with-apple-id.html).
2. **Register test devices on the Apple Developer Portal**: If this input is set, the Step will register the known test devices on Bitrise from team members with the Apple Developer Portal. Note that setting this to `yes` may cause devices to be registered against your limited quantity of test devices in the Apple Developer Portal, which can only be removed once annually during your renewal window.
3. **The minimum days the Provisioning Profile should be valid**: If this input is set to >0, the managed Provisioning Profile will be renewed if it expires within the configured number of days. Otherwise the Step renews the managed Provisioning Profile if it is expired.
4. The **Code signing certificate URL**, the **Code signing certificate passphrase**, the **Keychain path**, and the **Keychain password** inputs are automatically populated if certificates are uploaded to Bitrise's **Code Signing** tab. If you store your files in a private repo, you can manually edit these fields.

Under **IPA export configuration**:
1. **Developer Portal team**: Add the Developer Portal team's name to use for this export. This input defaults to the team used to build the archive.
2. **Rebuild from bitcode**: For non-App Store exports, should Xcode re-compile the app from bitcode?
3. **Include bitcode**: For App Store exports, should the package include bitcode?
4. **iCloud container environment**: If the app is using CloudKit, this input configures the `com.apple.developer.icloud-container-environment` entitlement. Available options vary depending on the type of provisioning profile used, but may include: `Development` and `Production`.
5. **Export options plist content**: Specifies a `plist` file content that configures archive exporting. If not specified, the Step will auto-generate it.

Under Debugging:
1. **Verbose logging***: You can set this input to `yes` to produce more informative logs.
</details>

## 🧩 Get started

Add this step directly to your workflow in the [Bitrise Workflow Editor](https://devcenter.bitrise.io/steps-and-workflows/steps-and-workflows-index/).

You can also run this step directly with [Bitrise CLI](https://github.com/bitrise-io/bitrise).

### Example

Archive, then export both development and app-store IPAs:

```yaml
steps:
- xcode-archive:
    title: Archive and export development IPA
    inputs:
    - export_method: development
- export-xcarchive:
    title: Export app-store IPA
    inputs:
    - archive_path: $BITRISE_XCARCHIVE_PATH # this env var is an output of the previous xcode-archive step
    - export_method: app-store
- deploy-to-bitrise-io: # deploy both IPAs as build artifacts
- deploy-to-itunesconnect-application-loader: # deploy the app-store IPA
```

## ⚙️ Configuration

<details>
<summary>Inputs</summary>

| Key | Description | Flags | Default |
| --- | --- | --- | --- |
| `archive_path` | Specifies the archive that should be exported.  The input value sets xcodebuild's `-archivePath` option. | required | `$BITRISE_XCARCHIVE_PATH` |
| `product` | Describes which product to export. | required | `app` |
| `distribution_method` | Describes how Xcode should export the archive. | required | `development` |
| `automatic_code_signing` | This input determines which Bitrise Apple service connection should be used for automatic code signing.  Available values: - `off`: Do not do any auto code signing. - `api-key`: [Bitrise Apple Service connection with API Key](https://devcenter.bitrise.io/getting-started/connecting-to-services/setting-up-connection-to-an-apple-service-with-api-key/). - `apple-id`: [Bitrise Apple Service connection with Apple ID](https://devcenter.bitrise.io/getting-started/connecting-to-services/connecting-to-an-apple-service-with-apple-id/). | required | `off` |
| `register_test_devices` | If this input is set, the Step will register the known test devices on Bitrise from team members with the Apple Developer Portal.  Note that setting this to yes may cause devices to be registered against your limited quantity of test devices in the Apple Developer Portal, which can only be removed once annually during your renewal window. | required | `no` |
| `min_profile_validity` | If this input is set to >0, the managed Provisioning Profile will be renewed if it expires within the configured number of days.  Otherwise the Step renews the managed Provisioning Profile if it is expired. | required | `0` |
| `certificate_url_list` | URL of the code signing certificate to download.  Multiple URLs can be specified, separated by a pipe (`\|`) character.  Local file path can be specified, using the `file://` URL scheme. | required, sensitive | `$BITRISE_CERTIFICATE_URL` |
| `passphrase_list` | Passphrases for the provided code signing certificates.  Specify as many passphrases as many Code signing certificate URL provided, separated by a pipe (`\|`) character.  Certificates without a passphrase: for using a single certificate, leave this step input empty. For multiple certificates, use the separator as if there was a passphrase (examples: `pass\|`, `\|pass\|`, `\|`) | sensitive | `$BITRISE_CERTIFICATE_PASSPHRASE` |
| `keychain_path` | Path to the Keychain where the code signing certificates will be installed. | required | `$HOME/Library/Keychains/login.keychain` |
| `keychain_password` | Password for the provided Keychain. | required, sensitive | `$BITRISE_KEYCHAIN_PASSWORD` |
| `export_development_team` | The Developer Portal team to use for this export.  Defaults to the team used to build the archive.  Defining this is also required when Automatic Code Signing is set to `apple-id` and the connected account belongs to multiple teams. |  |  |
| `compile_bitcode` | For __non-App Store__ exports, should Xcode re-compile the app from bitcode? | required | `yes` |
| `upload_bitcode` | For __App Store__ exports, should the package include bitcode? | required | `yes` |
| `manage_version_and_build_number` | Should Xcode manage the app's build number when uploading to App Store Connect. This will change the version and build numbers of all content in your app only if the is an invalid number (like one that was used previously or precedes your current build number). The input will not work if `export options plist content` input has been set. Default set to No. | required | `no` |
| `export_options_plist_content` | Specifies a plist file content that configures archive exporting.  If not specified, the Step will auto-generate it. |  |  |
| `verbose_log` | If this input is set, the Step will print additional logs for debugging. | required | `no` |
</details>

<details>
<summary>Outputs</summary>

| Environment Variable | Description |
| --- | --- |
| `BITRISE_IPA_PATH` | The created iOS or tvOS .ipa file's path. |
| `BITRISE_DSYM_PATH` | Step will collect every dsym (app dsym and framwork dsyms) in a directory, zip it and export the zipped directory path. |
| `BITRISE_IDEDISTRIBUTION_LOGS_PATH` | Path to the xcdistributionlogs zip |
</details>

## 🙋 Contributing

We welcome [pull requests](https://github.com/bitrise-steplib/steps-export-xcarchive/pulls) and [issues](https://github.com/bitrise-steplib/steps-export-xcarchive/issues) against this repository.

For pull requests, work on your changes in a forked repository and use the Bitrise CLI to [run step tests locally](https://devcenter.bitrise.io/bitrise-cli/run-your-first-build/).

Note: this step's end-to-end tests (defined in e2e/bitrise.yml) are working with secrets which are intentionally not stored in this repo. External contributors won't be able to run those tests. Don't worry, if you open a PR with your contribution, we will help with running tests and make sure that they pass.

Learn more about developing steps:

- [Create your own step](https://devcenter.bitrise.io/contributors/create-your-own-step/)
- [Testing your Step](https://devcenter.bitrise.io/contributors/testing-and-versioning-your-steps/)
