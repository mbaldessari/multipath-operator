package controllers

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	multipathv1 "github.com/multipath-operator/api/v1"
)

var _ = Describe("Multipath Integration Tests", func() {

	var (
		MultipathNamespace = "multipath-system"
		timeout            = time.Second * 30
		interval           = time.Millisecond * 250
	)

	Context("End-to-End Integration Tests", func() {
		var ctx context.Context
		var reconciler *MultipathReconciler
		var multipath *multipathv1.Multipath
		var MultipathName string

		BeforeEach(func() {
			ctx = context.Background()
			reconciler = &MultipathReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				IsOpenShift: false, // For testing, assume non-OpenShift
			}

			// Create unique name for each test
			MultipathName = fmt.Sprintf("test-multipath-integration-%d", rand.Intn(10000)) //nolint:gosec

			By("Creating the namespace if it doesn't exist")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: MultipathNamespace,
				},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating a comprehensive Multipath resource")
			multipath = &multipathv1.Multipath{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				},
				Spec: multipathv1.MultipathSpec{
					Config: multipathv1.MultipathConfig{
						Defaults: multipathv1.DefaultsConfig{
							UserFriendlyNames:  "yes",
							FindMultipaths:     "yes",
							PathGroupingPolicy: "failover",
							PathSelector:       "round-robin 0",
							PathChecker:        "tur",
							Failback:           "immediate",
							RRWeight:           "priorities",
							NoPathRetry:        "queue",
						},
						Blacklist: []multipathv1.BlacklistEntry{
							{
								Devnode: "^(ram|raw|loop|fd|md|dm-|sr|scd|st|dcssblk)[0-9]",
							},
							{
								WWID: "36001405blacklistedwwid",
							},
						},
						BlacklistExceptions: []multipathv1.BlacklistException{
							{
								Device: multipathv1.DeviceIdentifier{
									Vendor:  "NetApp",
									Product: "LUN.*",
								},
							},
						},
						Devices: []multipathv1.DeviceEntry{
							{
								Vendor:             "NetApp",
								Product:            "LUN",
								PathGroupingPolicy: "group_by_prio",
								PathSelector:       "round-robin 0",
								PathChecker:        "tur",
								Features:           "1 queue_if_no_path",
								HardwareHandler:    "1 alua",
							},
							{
								Vendor:  "EMC",
								Product: "SYMMETRIX",
							},
						},
						Multipaths: []multipathv1.MultipathEntry{
							{
								WWID:               "36001405abcdef1234567890",
								Alias:              "test-multipath-device",
								PathGroupingPolicy: "failover",
								PathSelector:       "round-robin 0",
								Failback:           "immediate",
							},
						},
					},
					SecurityContext: multipathv1.SecurityContextConfig{
						Privileged:               boolPtr(true),
						AllowPrivilegeEscalation: boolPtr(true),
						RunAsUser:                int64Ptr(0),
						SELinuxOptions: &multipathv1.SELinuxOptions{
							Level: "s0:c123,c456",
							Role:  "system_r",
							Type:  "spc_t",
							User:  "system_u",
						},
					},
					UpdateStrategy: multipathv1.UpdateStrategy{
						Type: "RollingUpdate",
						RollingUpdate: &multipathv1.RollingUpdateStrategy{
							MaxUnavailable: int32Ptr(1),
						},
					},
					NodeSelector: map[string]string{
						"kubernetes.io/os": "linux",
					},
				},
			}

			err = k8sClient.Create(ctx, multipath)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("Cleaning up the Multipath resource")
			err := k8sClient.Delete(ctx, multipath)
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Waiting for resource cleanup")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				}, &multipathv1.Multipath{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			if reconciler.IsOpenShift {
				By("Cleaning up OpenShift-specific resources")
				// Clean up SCC
				scc := &securityv1.SecurityContextConstraints{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: MultipathSCCName}, scc)
				if err == nil {
					_ = k8sClient.Delete(ctx, scc)
				}

				// Clean up ClusterRole
				clusterRole := &rbacv1.ClusterRole{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: MultipathSCCRoleName}, clusterRole)
				if err == nil {
					_ = k8sClient.Delete(ctx, clusterRole)
				}

				// Clean up ClusterRoleBinding
				clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: MultipathSCCBindingName}, clusterRoleBinding)
				if err == nil {
					_ = k8sClient.Delete(ctx, clusterRoleBinding)
				}
			}
		})

		It("should create all required resources in correct order", func() {
			Skip("Skipping integration test to avoid resource conflicts")
			By("Triggering reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap was created with correct content")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathConfigMapName,
					Namespace: MultipathNamespace,
				}, configMap)
			}, timeout, interval).Should(Succeed())

			Expect(configMap.Data).To(HaveKey("multipath.conf"))
			config := configMap.Data["multipath.conf"]
			Expect(config).To(ContainSubstring("user_friendly_names yes"))
			Expect(config).To(ContainSubstring("find_multipaths yes"))
			Expect(config).To(ContainSubstring("blacklist {"))
			Expect(config).To(ContainSubstring("blacklist_exceptions {"))
			Expect(config).To(ContainSubstring("devices {"))
			Expect(config).To(ContainSubstring("multipaths {"))
			Expect(config).To(ContainSubstring("vendor NetApp"))
			Expect(config).To(ContainSubstring("wwid 36001405abcdef1234567890"))

			if reconciler.IsOpenShift {
				By("Verifying SCC was created with correct privileges")
				scc := &securityv1.SecurityContextConstraints{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{
						Name: MultipathSCCName,
					}, scc)
				}, timeout, interval).Should(Succeed())

				Expect(scc.AllowPrivilegedContainer).To(BeTrue())
				Expect(scc.AllowHostNetwork).To(BeTrue())
				Expect(scc.AllowHostPID).To(BeTrue())
				Expect(scc.AllowHostDirVolumePlugin).To(BeTrue())
				Expect(*scc.AllowPrivilegeEscalation).To(BeTrue())
				Expect(scc.RunAsUser.Type).To(Equal(securityv1.RunAsUserStrategyRunAsAny))
				Expect(scc.SELinuxContext.Type).To(Equal(securityv1.SELinuxStrategyRunAsAny))
				Expect(scc.Users).To(ContainElement("system:serviceaccount:multipath-system:multipath-daemon"))

				By("Verifying SCC ClusterRole was created")
				clusterRole := &rbacv1.ClusterRole{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{
						Name: MultipathSCCRoleName,
					}, clusterRole)
				}, timeout, interval).Should(Succeed())

				Expect(clusterRole.Rules).To(HaveLen(1))
				rule := clusterRole.Rules[0]
				Expect(rule.APIGroups).To(ContainElement("security.openshift.io"))
				Expect(rule.Resources).To(ContainElement("securitycontextconstraints"))
				Expect(rule.Verbs).To(ContainElement("use"))
				Expect(rule.ResourceNames).To(ContainElement(MultipathSCCName))

				By("Verifying SCC ClusterRoleBinding was created")
				clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{
						Name: MultipathSCCBindingName,
					}, clusterRoleBinding)
				}, timeout, interval).Should(Succeed())

				Expect(clusterRoleBinding.RoleRef.Name).To(Equal(MultipathSCCRoleName))
				Expect(clusterRoleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
				Expect(clusterRoleBinding.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
				Expect(clusterRoleBinding.Subjects).To(HaveLen(1))
				subject := clusterRoleBinding.Subjects[0]
				Expect(subject.Kind).To(Equal("ServiceAccount"))
				Expect(subject.Name).To(Equal("multipath-daemon"))
				Expect(subject.Namespace).To(Equal(MultipathNamespace))
			} else {
				By("Skipping SCC verification (not on OpenShift)")
			}

			By("Verifying DaemonSet was created with correct configuration")
			daemonSet := &appsv1.DaemonSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
			}, timeout, interval).Should(Succeed())

			// Verify DaemonSet properties
			Expect(daemonSet.Spec.Template.Spec.ServiceAccountName).To(Equal("multipath-daemon"))
			Expect(daemonSet.Spec.Template.Spec.HostPID).To(BeTrue())
			Expect(daemonSet.Spec.Template.Spec.HostNetwork).To(BeTrue())
			Expect(daemonSet.Spec.Template.Spec.NodeSelector).To(Equal(map[string]string{
				"kubernetes.io/os": "linux",
			}))

			// Verify container configuration
			containers := daemonSet.Spec.Template.Spec.Containers
			Expect(containers).To(HaveLen(1))
			container := containers[0]
			Expect(container.Name).To(Equal("multipath-daemon"))
			Expect(container.Image).To(Equal(MultipathDaemonImage))

			// Verify security context
			secCtx := container.SecurityContext
			Expect(secCtx.Privileged).To(Equal(boolPtr(true)))
			Expect(secCtx.AllowPrivilegeEscalation).To(Equal(boolPtr(true)))
			Expect(secCtx.RunAsUser).To(Equal(int64Ptr(0)))
			Expect(secCtx.SELinuxOptions.Level).To(Equal("s0:c123,c456"))
			Expect(secCtx.SELinuxOptions.Role).To(Equal("system_r"))
			Expect(secCtx.SELinuxOptions.Type).To(Equal("spc_t"))
			Expect(secCtx.SELinuxOptions.User).To(Equal("system_u"))

			// Verify volumes and volume mounts
			Expect(container.VolumeMounts).To(HaveLen(5))
			Expect(daemonSet.Spec.Template.Spec.Volumes).To(HaveLen(5))

			// Verify environment variables
			Expect(container.Env).To(HaveLen(1))
			Expect(container.Env[0].Name).To(Equal("NODE_NAME"))
			Expect(container.Env[0].ValueFrom.FieldRef.FieldPath).To(Equal("spec.nodeName"))

			By("Verifying owner references are set correctly")
			Expect(configMap.OwnerReferences).To(HaveLen(1))
			Expect(configMap.OwnerReferences[0].Name).To(Equal(MultipathName))
			Expect(configMap.OwnerReferences[0].Kind).To(Equal("Multipath"))

			Expect(daemonSet.OwnerReferences).To(HaveLen(1))
			Expect(daemonSet.OwnerReferences[0].Name).To(Equal(MultipathName))
		})

		It("should handle resource updates correctly", func() {
			Skip("Skipping integration test to avoid resource conflicts")
			By("Initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial resources to be created")
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathConfigMapName,
					Namespace: MultipathNamespace,
				}, configMap)
			}, timeout, interval).Should(Succeed())

			By("Updating the Multipath resource configuration")
			Eventually(func() error {
				// Get the latest version of the resource
				latestMultipath := &multipathv1.Multipath{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				}, latestMultipath)
				if err != nil {
					return err
				}

				// Update configuration
				latestMultipath.Spec.Config.Defaults.UserFriendlyNames = "no"
				latestMultipath.Spec.Config.Defaults.PathChecker = "directio"
				latestMultipath.Spec.NodeSelector = map[string]string{
					"kubernetes.io/os": "linux",
					"node-type":        "worker",
				}

				return k8sClient.Update(ctx, latestMultipath)
			}, timeout, interval).Should(Succeed())

			By("Triggering reconciliation after update")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap was updated")
			configMap := &corev1.ConfigMap{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathConfigMapName,
					Namespace: MultipathNamespace,
				}, configMap)
				if err != nil {
					return ""
				}
				return configMap.Data["multipath.conf"]
			}, timeout, interval).Should(And(
				ContainSubstring("user_friendly_names no"),
				ContainSubstring("path_checker directio"),
			))

			By("Verifying DaemonSet was updated")
			daemonSet := &appsv1.DaemonSet{}
			Eventually(func() map[string]string {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
				if err != nil {
					return nil
				}
				return daemonSet.Spec.Template.Spec.NodeSelector
			}, timeout, interval).Should(Equal(map[string]string{
				"kubernetes.io/os": "linux",
				"node-type":        "worker",
			}))
		})

		It("should handle resource deletion and cleanup", func() {
			Skip("Skipping integration test to avoid resource conflicts")
			By("Initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying all resources exist")
			Eventually(func() error {
				configMap := &corev1.ConfigMap{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathConfigMapName,
					Namespace: MultipathNamespace,
				}, configMap)
			}, timeout, interval).Should(Succeed())

			Eventually(func() error {
				daemonSet := &appsv1.DaemonSet{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
			}, timeout, interval).Should(Succeed())

			By("Deleting the Multipath resource")
			Eventually(func() error {
				// Get the latest version before deleting
				latestMultipath := &multipathv1.Multipath{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				}, latestMultipath)
				if err != nil {
					return err
				}
				return k8sClient.Delete(ctx, latestMultipath)
			}, timeout, interval).Should(Succeed())

			By("Triggering reconciliation after deletion")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying resources are cleaned up due to owner references")
			Eventually(func() bool {
				configMap := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathConfigMapName,
					Namespace: MultipathNamespace,
				}, configMap)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			Eventually(func() bool {
				daemonSet := &appsv1.DaemonSet{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})
})
