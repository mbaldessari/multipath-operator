package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	multipathv1 "github.com/multipath-operator/api/v1"
)

const (
	MultipathFinalizer      = "multipath.io/finalizer"
	MultipathConfigMapName  = "multipath-config"
	MultipathDaemonSetName  = "multipath-daemon"
	MultipathNamespace      = "multipath-system"
	MultipathSCCName        = "multipath-daemon-scc"
	MultipathSCCRoleName    = "multipath-scc-user"
	MultipathSCCBindingName = "multipath-scc-binding"
	MultipathDaemonImage    = "quay.io/rhn_support_mbaldess/multipath-daemon:latest"

	configBlockEnd   = "}\n\n"
	deviceBlockStart = "    device {\n"
	deviceBlockEnd   = "    }\n"
)

type MultipathReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	IsOpenShift bool
}

// +kubebuilder:rbac:groups=multipath.io,resources=multipaths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=multipath.io,resources=multipaths/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=multipath.io,resources=multipaths/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;patch;delete;use
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

func (r *MultipathReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var multipath multipathv1.Multipath
	if err := r.Get(ctx, req.NamespacedName, &multipath); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Multipath resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Multipath")
		return ctrl.Result{}, err
	}

	if multipath.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&multipath, MultipathFinalizer) {
			controllerutil.AddFinalizer(&multipath, MultipathFinalizer)
			return ctrl.Result{}, r.Update(ctx, &multipath)
		}
	} else {
		if controllerutil.ContainsFinalizer(&multipath, MultipathFinalizer) {
			if err := r.finalizeMultipath(ctx, &multipath); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(&multipath, MultipathFinalizer)
			return ctrl.Result{}, r.Update(ctx, &multipath)
		}
		return ctrl.Result{}, nil
	}

	if err := r.reconcileConfigMap(ctx, &multipath); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		return ctrl.Result{}, err
	}

	if r.IsOpenShift {
		if err := r.reconcileSCC(ctx, &multipath); err != nil {
			logger.Error(err, "Failed to reconcile SCC")
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileDaemonSet(ctx, &multipath); err != nil {
		logger.Error(err, "Failed to reconcile DaemonSet")
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, &multipath); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *MultipathReconciler) reconcileConfigMap(ctx context.Context, multipath *multipathv1.Multipath) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MultipathConfigMapName,
			Namespace: MultipathNamespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Data = map[string]string{
			"multipath.conf": r.generateMultipathConfig(multipath),
		}
		return controllerutil.SetControllerReference(multipath, configMap, r.Scheme)
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		log.FromContext(ctx).Info("ConfigMap reconciled", "operation", op)
	}

	return nil
}

func (r *MultipathReconciler) reconcileDaemonSet(ctx context.Context, multipath *multipathv1.Multipath) error {
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MultipathDaemonSetName,
			Namespace: MultipathNamespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, daemonSet, func() error {
		r.setDaemonSetSpec(daemonSet, multipath)
		return controllerutil.SetControllerReference(multipath, daemonSet, r.Scheme)
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		log.FromContext(ctx).Info("DaemonSet reconciled", "operation", op)
	}

	return nil
}

func (r *MultipathReconciler) reconcileSCC(ctx context.Context, multipath *multipathv1.Multipath) error {
	// Create SCC
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{
			Name: MultipathSCCName,
			Annotations: map[string]string{
				"kubernetes.io/description": "Security context constraints for multipath daemon requiring privileged access to manage multipath devices",
			},
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, scc, func() error {
		r.setSCCSpec(scc)
		return controllerutil.SetControllerReference(multipath, scc, r.Scheme)
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		log.FromContext(ctx).Info("SCC reconciled", "operation", op)
	}

	// Create ClusterRole for SCC usage
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: MultipathSCCRoleName,
		},
	}

	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, clusterRole, func() error {
		clusterRole.Rules = []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"security.openshift.io"},
				Resources:     []string{"securitycontextconstraints"},
				Verbs:         []string{"use"},
				ResourceNames: []string{MultipathSCCName},
			},
		}
		return controllerutil.SetControllerReference(multipath, clusterRole, r.Scheme)
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		log.FromContext(ctx).Info("SCC ClusterRole reconciled", "operation", op)
	}

	// Create ClusterRoleBinding
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: MultipathSCCBindingName,
		},
	}

	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, clusterRoleBinding, func() error {
		clusterRoleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     MultipathSCCRoleName,
		}
		clusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "multipath-daemon",
				Namespace: MultipathNamespace,
			},
		}
		return controllerutil.SetControllerReference(multipath, clusterRoleBinding, r.Scheme)
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		log.FromContext(ctx).Info("SCC ClusterRoleBinding reconciled", "operation", op)
	}

	return nil
}

func (r *MultipathReconciler) setSCCSpec(scc *securityv1.SecurityContextConstraints) {
	allowHostDirVolumePlugin := true
	allowHostIPC := false
	allowHostNetwork := true
	allowHostPID := true
	allowHostPorts := false
	allowPrivilegedContainer := true
	allowPrivilegeEscalation := true

	scc.AllowHostDirVolumePlugin = allowHostDirVolumePlugin
	scc.AllowHostIPC = allowHostIPC
	scc.AllowHostNetwork = allowHostNetwork
	scc.AllowHostPID = allowHostPID
	scc.AllowHostPorts = allowHostPorts
	scc.AllowPrivilegedContainer = allowPrivilegedContainer
	scc.AllowPrivilegeEscalation = &allowPrivilegeEscalation

	scc.AllowedCapabilities = nil
	scc.DefaultAddCapabilities = nil
	scc.RequiredDropCapabilities = nil

	scc.FSGroup = securityv1.FSGroupStrategyOptions{
		Type: securityv1.FSGroupStrategyRunAsAny,
	}

	scc.RunAsUser = securityv1.RunAsUserStrategyOptions{
		Type: securityv1.RunAsUserStrategyRunAsAny,
	}

	scc.SELinuxContext = securityv1.SELinuxContextStrategyOptions{
		Type: securityv1.SELinuxStrategyRunAsAny,
	}

	scc.SupplementalGroups = securityv1.SupplementalGroupsStrategyOptions{
		Type: securityv1.SupplementalGroupsStrategyRunAsAny,
	}

	scc.Volumes = []securityv1.FSType{
		securityv1.FSTypeConfigMap,
		securityv1.FSTypeDownwardAPI,
		securityv1.FSTypeEmptyDir,
		securityv1.FSTypeHostPath,
		securityv1.FSTypePersistentVolumeClaim,
		securityv1.FSProjected,
		securityv1.FSTypeSecret,
	}

	scc.Users = []string{
		"system:serviceaccount:" + MultipathNamespace + ":multipath-daemon",
	}
}

func (r *MultipathReconciler) setDaemonSetSpec(daemonSet *appsv1.DaemonSet, multipath *multipathv1.Multipath) {
	labels := map[string]string{
		"app":        "multipath-daemon",
		"managed-by": "multipath-operator",
	}

	privileged := true
	hostPID := true
	hostNetwork := true

	if multipath.Spec.SecurityContext.Privileged != nil {
		privileged = *multipath.Spec.SecurityContext.Privileged
	}
	allowPrivilegeEscalation := true
	runAsUser := int64(0)

	securityContext := &corev1.SecurityContext{
		Privileged:               &privileged,
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		RunAsUser:                &runAsUser,
	}

	if multipath.Spec.SecurityContext.AllowPrivilegeEscalation != nil {
		securityContext.AllowPrivilegeEscalation = multipath.Spec.SecurityContext.AllowPrivilegeEscalation
	}

	if multipath.Spec.SecurityContext.RunAsUser != nil {
		securityContext.RunAsUser = multipath.Spec.SecurityContext.RunAsUser
	}

	if multipath.Spec.SecurityContext.SELinuxOptions != nil {
		securityContext.SELinuxOptions = &corev1.SELinuxOptions{
			Level: multipath.Spec.SecurityContext.SELinuxOptions.Level,
			Role:  multipath.Spec.SecurityContext.SELinuxOptions.Role,
			Type:  multipath.Spec.SecurityContext.SELinuxOptions.Type,
			User:  multipath.Spec.SecurityContext.SELinuxOptions.User,
		}
	}

	daemonSet.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
			Type: appsv1.RollingUpdateDaemonSetStrategyType,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "multipath-daemon",
				HostPID:            hostPID,
				HostNetwork:        hostNetwork,
				NodeSelector:       multipath.Spec.NodeSelector,
				Tolerations: []corev1.Toleration{
					{
						Key:    "node-role.kubernetes.io/master",
						Effect: corev1.TaintEffectNoSchedule,
					},
					{
						Key:    "node-role.kubernetes.io/control-plane",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "multipath-daemon",
						Image:           MultipathDaemonImage,
						SecurityContext: securityContext,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "config",
								MountPath: "/etc/multipath",
							},
							{
								Name:      "dev",
								MountPath: "/dev",
							},
							{
								Name:      "sys",
								MountPath: "/sys",
							},
							{
								Name:      "lib-modules",
								MountPath: "/lib/modules",
								ReadOnly:  true,
							},
							{
								Name:      "run-udev",
								MountPath: "/run/udev",
							},
						},
						Env: []corev1.EnvVar{
							{
								Name: "NODE_NAME",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "spec.nodeName",
									},
								},
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: MultipathConfigMapName,
								},
							},
						},
					},
					{
						Name: "dev",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/dev",
							},
						},
					},
					{
						Name: "sys",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/sys",
							},
						},
					},
					{
						Name: "lib-modules",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/lib/modules",
							},
						},
					},
					{
						Name: "run-udev",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/run/udev",
							},
						},
					},
				},
			},
		},
	}

	if multipath.Spec.UpdateStrategy.Type != "" {
		if multipath.Spec.UpdateStrategy.Type == "OnDelete" {
			daemonSet.Spec.UpdateStrategy.Type = appsv1.OnDeleteDaemonSetStrategyType
		}
		if multipath.Spec.UpdateStrategy.RollingUpdate != nil {
			var maxUnavailable *intstr.IntOrString
			if multipath.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable != nil {
				maxUnavailable = &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: *multipath.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable,
				}
			}
			daemonSet.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateDaemonSet{
				MaxUnavailable: maxUnavailable,
			}
		}
	}
}

func (r *MultipathReconciler) generateMultipathConfig(multipath *multipathv1.Multipath) string {
	config := "# Multipath configuration generated by multipath-operator\n\n"

	if !reflect.DeepEqual(multipath.Spec.Config.Defaults, multipathv1.DefaultsConfig{}) {
		config += "defaults {\n"
		defaults := multipath.Spec.Config.Defaults

		if defaults.UserFriendlyNames != "" {
			config += fmt.Sprintf("    user_friendly_names %s\n", defaults.UserFriendlyNames)
		}
		if defaults.FindMultipaths != "" {
			config += fmt.Sprintf("    find_multipaths %s\n", defaults.FindMultipaths)
		}
		if defaults.PathGroupingPolicy != "" {
			config += fmt.Sprintf("    path_grouping_policy %s\n", defaults.PathGroupingPolicy)
		}
		if defaults.PathSelector != "" {
			config += fmt.Sprintf("    path_selector %q\n", defaults.PathSelector)
		}
		if defaults.PathChecker != "" {
			config += fmt.Sprintf("    path_checker %s\n", defaults.PathChecker)
		}
		if defaults.Failback != "" {
			config += fmt.Sprintf("    failback %s\n", defaults.Failback)
		}
		if defaults.RRWeight != "" {
			config += fmt.Sprintf("    rr_weight %s\n", defaults.RRWeight)
		}
		if defaults.NoPathRetry != "" {
			config += fmt.Sprintf("    no_path_retry %s\n", defaults.NoPathRetry)
		}

		config += configBlockEnd
	}

	config += r.generateBlacklistConfig(multipath.Spec.Config.Blacklist, "blacklist")
	config += r.generateBlacklistExceptionConfig(multipath.Spec.Config.BlacklistExceptions, "blacklist_exceptions")

	if len(multipath.Spec.Config.Devices) > 0 {
		config += "devices {\n"
		for i := range multipath.Spec.Config.Devices {
			device := &multipath.Spec.Config.Devices[i]
			config += deviceBlockStart
			config += fmt.Sprintf("        vendor %s\n", device.Vendor)
			config += fmt.Sprintf("        product %s\n", device.Product)

			if device.PathGroupingPolicy != "" {
				config += fmt.Sprintf("        path_grouping_policy %s\n", device.PathGroupingPolicy)
			}
			if device.PathSelector != "" {
				config += fmt.Sprintf("        path_selector %q\n", device.PathSelector)
			}
			if device.PathChecker != "" {
				config += fmt.Sprintf("        path_checker %s\n", device.PathChecker)
			}
			if device.Features != "" {
				features := r.parseFeatureValue(device.Features)
				config += fmt.Sprintf("        features %s\n", features)
			}
			if device.HardwareHandler != "" {
				handler := r.parseFeatureValue(device.HardwareHandler)
				config += fmt.Sprintf("        hardware_handler %s\n", handler)
			}

			config += deviceBlockEnd
		}
		config += configBlockEnd
	}

	if len(multipath.Spec.Config.Multipaths) > 0 {
		config += "multipaths {\n"
		for _, mp := range multipath.Spec.Config.Multipaths {
			config += "    multipath {\n"
			config += fmt.Sprintf("        wwid %s\n", mp.WWID)

			if mp.Alias != "" {
				config += fmt.Sprintf("        alias %s\n", mp.Alias)
			}
			if mp.PathGroupingPolicy != "" {
				config += fmt.Sprintf("        path_grouping_policy %s\n", mp.PathGroupingPolicy)
			}
			if mp.PathSelector != "" {
				config += fmt.Sprintf("        path_selector %q\n", mp.PathSelector)
			}
			if mp.Failback != "" {
				config += fmt.Sprintf("        failback %s\n", mp.Failback)
			}

			config += "    }\n"
		}
		config += "}\n"
	}

	return config
}

func (r *MultipathReconciler) parseFeatureValue(value string) string {
	parts := strings.Fields(value)
	if len(parts) > 1 {
		// For features and hardware_handler, skip the count and return the rest
		return strings.Join(parts[1:], " ")
	}
	return value
}

func (r *MultipathReconciler) addDeviceBlock(config, devnode, wwid string, device multipathv1.DeviceIdentifier) string {
	if devnode != "" {
		config += fmt.Sprintf("    devnode %s\n", devnode)
	}
	if wwid != "" {
		config += fmt.Sprintf("    wwid %s\n", wwid)
	}
	if device.Vendor != "" || device.Product != "" {
		config += deviceBlockStart
		if device.Vendor != "" {
			config += fmt.Sprintf("        vendor %s\n", device.Vendor)
		}
		if device.Product != "" {
			config += fmt.Sprintf("        product %s\n", device.Product)
		}
		config += deviceBlockEnd
	}
	return config
}

func (r *MultipathReconciler) generateBlacklistConfig(entries []multipathv1.BlacklistEntry, blockName string) string {
	if len(entries) == 0 {
		return ""
	}

	config := blockName + " {\n"
	for _, entry := range entries {
		config = r.addDeviceBlock(config, entry.Devnode, entry.WWID, entry.Device)
	}
	config += configBlockEnd
	return config
}

func (r *MultipathReconciler) generateBlacklistExceptionConfig(entries []multipathv1.BlacklistException, blockName string) string {
	if len(entries) == 0 {
		return ""
	}

	config := blockName + " {\n"
	for _, entry := range entries {
		config = r.addDeviceBlock(config, entry.Devnode, entry.WWID, entry.Device)
	}
	config += configBlockEnd
	return config
}

func (r *MultipathReconciler) updateStatus(ctx context.Context, multipath *multipathv1.Multipath) error {
	var daemonSet appsv1.DaemonSet
	err := r.Get(ctx, types.NamespacedName{
		Name:      MultipathDaemonSetName,
		Namespace: MultipathNamespace,
	}, &daemonSet)

	if err != nil {
		if errors.IsNotFound(err) {
			multipath.Status.Phase = "Pending"
			multipath.Status.Ready = false
			multipath.Status.NodesReady = 0
			multipath.Status.NodesTotal = 0
			multipath.Status.Message = "DaemonSet not found"
		} else {
			return err
		}
	} else {
		multipath.Status.NodesTotal = daemonSet.Status.DesiredNumberScheduled
		multipath.Status.NodesReady = daemonSet.Status.NumberReady

		if daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled && daemonSet.Status.DesiredNumberScheduled > 0 {
			multipath.Status.Phase = "Running"
			multipath.Status.Ready = true
			multipath.Status.Message = "All nodes are ready"
		} else if daemonSet.Status.NumberReady > 0 {
			multipath.Status.Phase = "Progressing"
			multipath.Status.Ready = false
			multipath.Status.Message = fmt.Sprintf("%d/%d nodes ready", daemonSet.Status.NumberReady, daemonSet.Status.DesiredNumberScheduled)
		} else {
			multipath.Status.Phase = "Pending"
			multipath.Status.Ready = false
			multipath.Status.Message = "No nodes ready"
		}
	}

	return r.Status().Update(ctx, multipath)
}

func (r *MultipathReconciler) finalizeMultipath(ctx context.Context, _ *multipathv1.Multipath) error {
	log.FromContext(ctx).Info("Finalizing Multipath resource")
	return nil
}

func (r *MultipathReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&multipathv1.Multipath{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.ConfigMap{})

	if r.IsOpenShift {
		builder = builder.
			Owns(&securityv1.SecurityContextConstraints{}).
			Owns(&rbacv1.ClusterRole{}).
			Owns(&rbacv1.ClusterRoleBinding{})
	}

	return builder.Complete(r)
}
