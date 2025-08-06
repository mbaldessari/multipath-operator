package controllers

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	multipathv1 "github.com/multipath-operator/api/v1"
)

var _ = Describe("MultipathController", func() {

	const (
		MultipathName      = "test-multipath"
		MultipathNamespace = "multipath-system"
		timeout            = time.Second * 10
		duration           = time.Second * 10
		interval           = time.Millisecond * 250
	)

	Context("When reconciling a Multipath resource", func() {
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      MultipathName,
			Namespace: MultipathNamespace,
		}

		multipath := &multipathv1.Multipath{}

		BeforeEach(func() {
			By("Creating the namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: MultipathNamespace,
				},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating the custom resource for the Kind Multipath")
			multipath = &multipathv1.Multipath{
				ObjectMeta: metav1.ObjectMeta{
					Name:      MultipathName,
					Namespace: MultipathNamespace,
				},
				Spec: multipathv1.MultipathSpec{
					Config: getCompleteMultipathConfig(),
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
			By("Cleanup the specific resource instance Multipath")
			resource := &multipathv1.Multipath{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Multipath")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			Skip("Skipping main controller test to avoid conflicts with running controller manager")
		})
	})

	Context("When testing individual reconcile functions", func() {
		var reconciler *MultipathReconciler
		var ctx context.Context
		var multipath *multipathv1.Multipath

		BeforeEach(func() {
			ctx = context.Background()
			reconciler = &MultipathReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				IsOpenShift: false, // For testing, assume non-OpenShift
			}

			// Create namespace if it doesn't exist
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: MultipathNamespace,
				},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			// Create a unique multipath resource for each test
			testName := fmt.Sprintf("test-multipath-%d", rand.Intn(10000)) //nolint:gosec
			multipath = &multipathv1.Multipath{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: MultipathNamespace,
				},
				Spec: multipathv1.MultipathSpec{
					Config: multipathv1.MultipathConfig{
						Defaults: multipathv1.DefaultsConfig{
							UserFriendlyNames:  "yes",
							FindMultipaths:     "yes",
							PathGroupingPolicy: "failover",
						},
					},
				},
			}
		})

		AfterEach(func() {
			// Clean up any remaining resources
			if multipath != nil {
				_ = k8sClient.Delete(ctx, multipath)
			}
			// Clean up ConfigMap
			configMap := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      MultipathConfigMapName,
				Namespace: MultipathNamespace,
			}, configMap); err == nil {
				_ = k8sClient.Delete(ctx, configMap)
			}
			// Clean up DaemonSet
			daemonSet := &appsv1.DaemonSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      MultipathDaemonSetName,
				Namespace: MultipathNamespace,
			}, daemonSet); err == nil {
				_ = k8sClient.Delete(ctx, daemonSet)
			}
		})

		Describe("reconcileConfigMap", func() {
			It("should create a ConfigMap with correct multipath configuration", func() {
				Skip("Skipping to avoid conflicts with running controller manager")
			})
		})

		Describe("reconcileSCC", func() {
			It("should create SCC, ClusterRole, and ClusterRoleBinding when on OpenShift", func() {
				Skip("Skipping SCC tests in non-OpenShift environment")
			})
		})

		Describe("reconcileDaemonSet", func() {
			It("should create a DaemonSet with correct configuration", func() {
				By("Creating multipath resource first")
				err := k8sClient.Create(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				By("Reconciling DaemonSet")
				err = reconciler.reconcileDaemonSet(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				daemonSet := &appsv1.DaemonSet{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
				Expect(err).NotTo(HaveOccurred())

				By("Checking DaemonSet labels")
				expectedLabels := map[string]string{
					"app":        "multipath-daemon",
					"managed-by": "multipath-operator",
				}
				Expect(daemonSet.Spec.Template.Labels).To(Equal(expectedLabels))

				By("Checking container configuration")
				containers := daemonSet.Spec.Template.Spec.Containers
				Expect(containers).To(HaveLen(1))
				container := containers[0]
				Expect(container.Name).To(Equal("multipath-daemon"))
				Expect(container.Image).To(Equal(MultipathDaemonImage))

				By("Checking security context")
				Expect(container.SecurityContext.Privileged).To(Equal(boolPtr(true)))
				Expect(container.SecurityContext.AllowPrivilegeEscalation).To(Equal(boolPtr(true)))
				Expect(container.SecurityContext.RunAsUser).To(Equal(int64Ptr(0)))

				By("Checking volume mounts")
				expectedVolumeMounts := []string{"config", "dev", "sys", "lib-modules", "run-udev"}
				Expect(container.VolumeMounts).To(HaveLen(len(expectedVolumeMounts)))
				for _, expectedMount := range expectedVolumeMounts {
					found := false
					for _, mount := range container.VolumeMounts {
						if mount.Name == expectedMount {
							found = true
							break
						}
					}
					Expect(found).To(BeTrue(), "Expected volume mount %s not found", expectedMount)
				}

				By("Checking volumes")
				expectedVolumes := []string{"config", "dev", "sys", "lib-modules", "run-udev"}
				Expect(daemonSet.Spec.Template.Spec.Volumes).To(HaveLen(len(expectedVolumes)))
				for _, expectedVolume := range expectedVolumes {
					found := false
					for _, volume := range daemonSet.Spec.Template.Spec.Volumes {
						if volume.Name == expectedVolume {
							found = true
							break
						}
					}
					Expect(found).To(BeTrue(), "Expected volume %s not found", expectedVolume)
				}

				By("Checking host settings")
				Expect(daemonSet.Spec.Template.Spec.HostPID).To(BeTrue())
				Expect(daemonSet.Spec.Template.Spec.HostNetwork).To(BeTrue())
				Expect(daemonSet.Spec.Template.Spec.ServiceAccountName).To(Equal("multipath-daemon"))

			})

			It("should apply custom security context when specified", func() {
				By("Setting up custom security context")
				multipath.Spec.SecurityContext = multipathv1.SecurityContextConfig{
					Privileged:               boolPtr(false),
					AllowPrivilegeEscalation: boolPtr(false),
					RunAsUser:                int64Ptr(1000),
					SELinuxOptions: &multipathv1.SELinuxOptions{
						Level: "s0:c123,c456",
						Role:  "system_r",
						Type:  "spc_t",
						User:  "system_u",
					},
				}

				By("Creating multipath resource first")
				err := k8sClient.Create(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				By("Reconciling DaemonSet")
				err = reconciler.reconcileDaemonSet(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				daemonSet := &appsv1.DaemonSet{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
				Expect(err).NotTo(HaveOccurred())

				container := daemonSet.Spec.Template.Spec.Containers[0]
				Expect(container.SecurityContext.Privileged).To(Equal(boolPtr(false)))
				Expect(container.SecurityContext.AllowPrivilegeEscalation).To(Equal(boolPtr(false)))
				Expect(container.SecurityContext.RunAsUser).To(Equal(int64Ptr(1000)))
				Expect(container.SecurityContext.SELinuxOptions.Level).To(Equal("s0:c123,c456"))
				Expect(container.SecurityContext.SELinuxOptions.Role).To(Equal("system_r"))
				Expect(container.SecurityContext.SELinuxOptions.Type).To(Equal("spc_t"))
				Expect(container.SecurityContext.SELinuxOptions.User).To(Equal("system_u"))

			})

			It("should apply custom update strategy when specified", func() {
				By("Setting up custom update strategy")
				multipath.Spec.UpdateStrategy = multipathv1.UpdateStrategy{
					Type: "OnDelete",
				}

				By("Creating multipath resource first")
				err := k8sClient.Create(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				By("Reconciling DaemonSet")
				err = reconciler.reconcileDaemonSet(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				daemonSet := &appsv1.DaemonSet{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
				Expect(err).NotTo(HaveOccurred())

				Expect(daemonSet.Spec.UpdateStrategy.Type).To(Equal(appsv1.OnDeleteDaemonSetStrategyType))

			})

			It("should apply rolling update strategy with max unavailable", func() {
				By("Setting up rolling update strategy")
				multipath.Spec.UpdateStrategy = multipathv1.UpdateStrategy{
					Type: "RollingUpdate",
					RollingUpdate: &multipathv1.RollingUpdateStrategy{
						MaxUnavailable: int32Ptr(2),
					},
				}

				By("Creating multipath resource first")
				err := k8sClient.Create(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				By("Reconciling DaemonSet")
				err = reconciler.reconcileDaemonSet(ctx, multipath)
				Expect(err).NotTo(HaveOccurred())

				daemonSet := &appsv1.DaemonSet{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      MultipathDaemonSetName,
					Namespace: MultipathNamespace,
				}, daemonSet)
				Expect(err).NotTo(HaveOccurred())

				Expect(daemonSet.Spec.UpdateStrategy.Type).To(Equal(appsv1.RollingUpdateDaemonSetStrategyType))
				Expect(daemonSet.Spec.UpdateStrategy.RollingUpdate).NotTo(BeNil())
				Expect(daemonSet.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable).To(Equal(&intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 2,
				}))

			})
		})
	})
})

// Helper functions
func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}

func int32Ptr(i int32) *int32 {
	return &i
}
