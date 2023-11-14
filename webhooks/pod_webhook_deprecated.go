package webhooks

import (
	"context"
	"fmt"
	"reflect"

	v1alpha1 "github.com/thisthat/open-feature-operator/apis/core/v1alpha1"
	"github.com/thisthat/open-feature-operator/pkg/utils"
)

//nolint:gocyclo
func (m *PodMutator) handleFeatureFlagConfigurationAnnotation(ctx context.Context, fcConfig *v1alpha1.FlagSourceConfigurationSpec, ffconfigAnnotation string, defaultNamespace string) error {
	for _, ffName := range parseList(ffconfigAnnotation) {
		ns, name := utils.ParseAnnotation(ffName, defaultNamespace)
		fsConfig := m.getFeatureFlag(ctx, ns, name)
		if reflect.DeepEqual(fsConfig, v1alpha1.FeatureFlagConfiguration{}) {
			return fmt.Errorf("FeatureFlagConfiguration %s not found", ffName)
		}
		if fsConfig.Spec.FlagDSpec != nil {
			if len(fsConfig.Spec.FlagDSpec.Envs) > 0 {
				fcConfig.EnvVars = append(fsConfig.Spec.FlagDSpec.Envs, fcConfig.EnvVars...)
			}
			if fsConfig.Spec.FlagDSpec.MetricsPort != 0 && fcConfig.MetricsPort == v1alpha1.DefaultMetricPort {
				fcConfig.MetricsPort = fsConfig.Spec.FlagDSpec.MetricsPort
			}
		}
		switch {
		case fsConfig.Spec.SyncProvider == nil:
			fcConfig.Sources = append(fcConfig.Sources, v1alpha1.Source{
				Provider: fcConfig.DefaultSyncProvider,
				Source:   ffName,
			})
		case v1alpha1.SyncProviderType(fsConfig.Spec.SyncProvider.Name).IsKubernetes():
			fcConfig.Sources = append(fcConfig.Sources, v1alpha1.Source{
				Provider: v1alpha1.SyncProviderKubernetes,
				Source:   ffName,
			})
		case v1alpha1.SyncProviderType(fsConfig.Spec.SyncProvider.Name).IsFilepath():
			fcConfig.Sources = append(fcConfig.Sources, v1alpha1.Source{
				Provider: v1alpha1.SyncProviderFilepath,
				Source:   ffName,
			})
		case v1alpha1.SyncProviderType(fsConfig.Spec.SyncProvider.Name).IsHttp():
			if fsConfig.Spec.SyncProvider.HttpSyncConfiguration == nil {
				return fmt.Errorf("FeatureFlagConfiguration %s is missing HttpSyncConfiguration", ffName)
			}
			fcConfig.Sources = append(fcConfig.Sources, v1alpha1.Source{
				Provider:            v1alpha1.SyncProviderHttp,
				Source:              fsConfig.Spec.SyncProvider.HttpSyncConfiguration.Target,
				HttpSyncBearerToken: fsConfig.Spec.SyncProvider.HttpSyncConfiguration.BearerToken,
			})
		default:
			return fmt.Errorf("FeatureFlagConfiguration %s configuration is unrecognized", ffName)
		}
	}
	return nil
}
