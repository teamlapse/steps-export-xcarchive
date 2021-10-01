# Export iOS and tvOS Xcode archive

[![Step changelog](https://shields.io/github/v/release/bitrise-steplib/steps-export-xcarchive?include_prereleases&label=changelog&color=blueviolet)](https://github.com/bitrise-steplib/steps-export-xcarchive/releases)

Export iOS and tvOS IPA from an existing Xcode archive

<details>
<summary>Description</summary>

Exports an IPA from an existing iOS and tvOS `.xcarchive` file.
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
| `archive_path` | Path to the iOS or tvOS archive (.xcarchive) which should be exported. | required | `$BITRISE_XCARCHIVE_PATH` |
| `product` | Describes which product to export. Possible options are App or App Clip. | required | `app` |
| `distribution_method` | Describes how Xcode should export the archive. | required |  |
| `export_development_team` | The Developer Portal team to use for this export.  Format example: `1MZX23ABCD4` |  |  |
| `compile_bitcode` | For __non-App Store__ exports, should Xcode re-compile the app from bitcode? | required | `yes` |
| `upload_bitcode` | For __App Store__ exports, should the package include bitcode? | required | `yes` |
| `custom_export_options_plist_content` | Specifies a custom export options plist content that configures archive exporting. If empty, step generates these options based on the embedded provisioning profile, with default values.  Call `xcodebuild -help` for available export options. |  |  |
| `verbose_log` | Enable verbose logging? | required | `yes` |
</details>

<details>
<summary>Outputs</summary>

| Environment Variable | Description |
| --- | --- |
| `BITRISE_IPA_PATH` |  |
| `BITRISE_DSYM_PATH` | Step will collect every dsym (app dsym and framwork dsyms) in a directory, zip it and export the zipped directory path. |
| `BITRISE_IDEDISTRIBUTION_LOGS_PATH` |  |
</details>

## 🙋 Contributing

We welcome [pull requests](https://github.com/bitrise-steplib/steps-export-xcarchive/pulls) and [issues](https://github.com/bitrise-steplib/steps-export-xcarchive/issues) against this repository.

For pull requests, work on your changes in a forked repository and use the Bitrise CLI to [run step tests locally](https://devcenter.bitrise.io/bitrise-cli/run-your-first-build/).

Note: this step's end-to-end tests (defined in e2e/bitrise.yml) are working with secrets which are intentionally not stored in this repo. External contributors won't be able to run those tests. Don't worry, if you open a PR with your contribution, we will help with running tests and make sure that they pass.

Learn more about developing steps:

- [Create your own step](https://devcenter.bitrise.io/contributors/create-your-own-step/)
- [Testing your Step](https://devcenter.bitrise.io/contributors/testing-and-versioning-your-steps/)
