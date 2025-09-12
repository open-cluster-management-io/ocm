package options

import (
	"testing"

	"k8s.io/client-go/kubernetes"
)

func TestGetImporterRenderers(t *testing.T) {
	type args struct {
		options           *Options
		operatorNamespace string
	}
	tests := []struct {
		name       string
		args       args
		wantNum    int
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "empty ImporterRendererList returns default renderers",
			args: args{
				options: &Options{
					APIServerURL:         "https://hub.example.com",
					AgentImage:           "test-image",
					BootstrapSA:          "test-sa",
					ImporterRendererList: []string{},
				},
				operatorNamespace: "test-ns",
			},
			wantNum: 3,
			wantErr: false,
		},
		{
			name: "ImporterRendererList contains RenderAuto",
			args: args{
				options: &Options{
					APIServerURL:         "https://hub.example.com",
					AgentImage:           "test-image",
					BootstrapSA:          "test-sa",
					ImporterRendererList: []string{RenderAuto},
				},
				operatorNamespace: "test-ns",
			},
			wantNum: 3,
			wantErr: false,
		},
		{
			name: "ImporterRendererList contains RenderFromConfigSecret",
			args: args{
				options: &Options{
					ImporterRendererList: []string{RenderFromConfigSecret},
				},
				operatorNamespace: "test-ns",
			},
			wantNum: 1,
			wantErr: false,
		},
		{
			name: "ImporterRendererList contains unknown renderer",
			args: args{
				options: &Options{
					ImporterRendererList: []string{"unknown-renderer"},
				},
				operatorNamespace: "test-ns",
			},
			wantNum:    0,
			wantErr:    true,
			wantErrMsg: "unknown importer renderer unknown-renderer",
		},
	}

	// Use a nil kubeClient since the renderer functions do not call methods in these tests
	var kubeClient kubernetes.Interface = nil

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderers, err := GetImporterRenderers(tt.args.options, kubeClient, tt.args.operatorNamespace)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.wantErrMsg != "" && err.Error() != tt.wantErrMsg {
					t.Errorf("expected error message %q, got %q", tt.wantErrMsg, err.Error())
				}
				if len(renderers) != tt.wantNum {
					t.Errorf("expected %d renderers, got %d", tt.wantNum, len(renderers))
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(renderers) != tt.wantNum {
					t.Errorf("expected %d renderers, got %d", tt.wantNum, len(renderers))
				}
			}
		})
	}
}
