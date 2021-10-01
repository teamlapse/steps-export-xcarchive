package main

import (
	"reflect"
	"testing"
)

func TestConfig_validate(t *testing.T) {
	type fields struct {
		ArchivePath                     string
		ExportMethod                    string
		UploadBitcode                   bool
		CompileBitcode                  bool
		TeamID                          string
		CustomExportOptionsPlistContent string
		UseLegacyExport                 bool
		DeployDir                       string
		VerboseLog                      bool
	}
	tests := []struct {
		name    string
		fields  fields
		want    fields
		wantErr bool
	}{
		{
			name: "TeamID contains whitespace",
			fields: fields{
				TeamID: "  ",
			},
			want: fields{
				TeamID: "",
			},
			wantErr: false,
		},
		{
			name: "ExportOptionsPlistContent contains whitespace",
			fields: fields{
				CustomExportOptionsPlistContent: "  ",
			},
			want: fields{
				CustomExportOptionsPlistContent: "",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs := &Config{
				ArchivePath:               tt.fields.ArchivePath,
				DistributionMethod:        tt.fields.ExportMethod,
				UploadBitcode:             tt.fields.UploadBitcode,
				CompileBitcode:            tt.fields.CompileBitcode,
				TeamID:                    tt.fields.TeamID,
				ExportOptionsPlistContent: tt.fields.CustomExportOptionsPlistContent,
				DeployDir:                 tt.fields.DeployDir,
				VerboseLog:                tt.fields.VerboseLog,
			}
			wantConfigs := &Config{
				ArchivePath:               tt.want.ArchivePath,
				DistributionMethod:        tt.want.ExportMethod,
				UploadBitcode:             tt.want.UploadBitcode,
				CompileBitcode:            tt.want.CompileBitcode,
				TeamID:                    tt.want.TeamID,
				ExportOptionsPlistContent: tt.want.CustomExportOptionsPlistContent,
				DeployDir:                 tt.want.DeployDir,
				VerboseLog:                tt.want.VerboseLog,
			}
			if err := configs.validate(); (err != nil) != tt.wantErr {
				t.Errorf("Config.validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(configs, wantConfigs) {
				t.Errorf("Config.validate() configs = %+v, wantConfig = %+v", configs, wantConfigs)
			}
		})
	}
}
