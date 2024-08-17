package export

import (
	"github.com/teamlapse/go-xcode/certificateutil"
	"github.com/teamlapse/go-xcode/profileutil"
)

// CodeSignGroup ...
type CodeSignGroup interface {
	Certificate() certificateutil.CertificateInfoModel
	InstallerCertificate() *certificateutil.CertificateInfoModel
	BundleIDProfileMap() map[string]profileutil.ProvisioningProfileInfoModel
}
