// Copyright (c) 2022 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package secret_test

import (
	"encoding/json"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ShootSecret controller tests", func() {
	var (
		resourceName string

		shoot      *gardencorev1beta1.Shoot
		shootState *gardencorev1alpha1.ShootState

		cluster *extensionsv1alpha1.Cluster
	)

	BeforeEach(func() {
		resourceName = "test-" + utils.ComputeSHA256Hex([]byte(CurrentSpecReport().LeafNodeLocation.String()))[:8]

		By("build shoot object")
		shoot = &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: testNamespace.Name,
			},
			Spec: gardencorev1beta1.ShootSpec{
				SecretBindingName: "my-provider-account",
				CloudProfileName:  "cloudprofile1",
				Region:            "europe-central-1",
				Provider: gardencorev1beta1.Provider{
					Type: "foo-provider",
					Workers: []gardencorev1beta1.Worker{
						{
							Name:    "cpu-worker",
							Minimum: 3,
							Maximum: 3,
							Machine: gardencorev1beta1.Machine{
								Type: "large",
							},
						},
					},
				},
				Kubernetes: gardencorev1beta1.Kubernetes{
					Version: "1.20.1",
				},
				Networking: gardencorev1beta1.Networking{
					Type: "foo-networking",
				},
			},
		}

		By("create shootstate")
		shootState = &gardencorev1alpha1.ShootState{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shoot.Name,
				Namespace: shoot.Namespace,
			},
		}
		Expect(testClient.Create(ctx, shootState)).To(Or(Succeed(), BeAlreadyExistsError()))
		log.Info("Created shootstate for test", "shootState", client.ObjectKeyFromObject(shootState))

		DeferCleanup(func() {
			By("delete shootstate")
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, shootState))).To(Succeed())
		})

		By("create cluster")
		cluster = &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: seedNamespace.Name,
			},
			Spec: extensionsv1alpha1.ClusterSpec{
				Shoot: runtime.RawExtension{
					Object: shoot,
				},
				CloudProfile: runtime.RawExtension{
					Object: &gardencorev1beta1.CloudProfile{},
				},
				Seed: runtime.RawExtension{
					Object: &gardencorev1beta1.Seed{},
				},
			},
		}
		Expect(testClient.Create(ctx, cluster)).To(Succeed())
		log.Info("Created cluster for test", "cluster", client.ObjectKeyFromObject(cluster))

		DeferCleanup(func() {
			By("delete cluster")
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, cluster))).To(Succeed())
		})
	})

	It("should sync relevant secrets to the shootstate", func() {
		By("create irrelevant secret")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: seedNamespace.Name,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"some": []byte("data"),
			},
		}
		Expect(testClient.Create(ctx, secret)).To(Succeed())

		By("verifying secret did not get synced to shootstate")
		Consistently(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).ShouldNot(containData(withName(secret.Name)))

		By("verifying secret has no finalizers")
		Consistently(func(g Gomega) []string {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			return secret.Finalizers
		}).Should(BeEmpty())

		By("updating irrelevant secret to stay irrelevant")
		patch := client.MergeFrom(secret.DeepCopy())
		metav1.SetMetaDataLabel(&secret.ObjectMeta, secretsmanager.LabelKeyManagedBy, secretsmanager.LabelValueSecretsManager)
		Expect(testClient.Patch(ctx, secret, patch)).To(Succeed())

		By("verifying secret did still not get synced to shootstate")
		Consistently(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).ShouldNot(containData(withName(secret.Name)))

		By("verifying secret has no finalizers")
		Consistently(func(g Gomega) []string {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			return secret.Finalizers
		}).Should(BeEmpty())

		By("updating irrelevant secret to become relevant")
		patch = client.MergeFrom(secret.DeepCopy())
		metav1.SetMetaDataLabel(&secret.ObjectMeta, secretsmanager.LabelKeyPersist, secretsmanager.LabelValueTrue)
		Expect(testClient.Patch(ctx, secret, patch)).To(Succeed())

		By("verifying secret did now get synced to shootstate")
		Eventually(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).Should(containData(
			withName(secret.Name),
			withType("secret"),
			withLabels(secret.Labels),
			withData(secret.Data),
		))

		By("verifying secret has finalizers")
		Eventually(func(g Gomega) []string {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			return secret.Finalizers
		}).Should(ConsistOf("gardenlet.gardener.cloud/secret-controller"))

		By("updating data of relevant secret")
		patch = client.MergeFrom(secret.DeepCopy())
		secret.Data["more"] = []byte("data")
		Expect(testClient.Patch(ctx, secret, patch)).To(Succeed())

		By("verifying secret did now get synced")
		Eventually(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).Should(containData(
			withName(secret.Name),
			withType("secret"),
			withLabels(secret.Labels),
			withData(secret.Data),
		))

		By("deleting relevant secret")
		Expect(testClient.Delete(ctx, secret)).To(Succeed())

		By("verifying secret got removed")
		Eventually(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).ShouldNot(containData(withName(secret.Name)))

		By("verifying secret has been removed from the system")
		Eventually(func() error {
			return testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)
		}).Should(BeNotFoundError())
	})

	It("should sync external secrets to the shootstate", func() {
		By("creating external secret")
		secret := newRelevantSecret(resourceName, seedNamespace.Name)
		metav1.SetMetaDataLabel(&secret.ObjectMeta, secretsmanager.LabelKeyManagerIdentity, "extension")
		Expect(testClient.Create(ctx, secret)).To(Succeed())

		By("verifying secret did get synced to shootstate")
		Eventually(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).Should(containData(
			withName(secret.Name),
			withType("secret"),
			withLabels(secret.Labels),
			withData(secret.Data),
		))

		By("verifying secret has finalizers")
		Eventually(func(g Gomega) []string {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			return secret.Finalizers
		}).Should(ConsistOf("gardenlet.gardener.cloud/secret-controller"))
	})

	It("should do nothing if the secret does not belong to a shoot namespace", func() {
		By("creating other namespace")
		nonShootNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}
		Expect(testClient.Create(ctx, nonShootNamespace)).To(Succeed())
		log.Info("Created other Namespace for test", "namespaceName", nonShootNamespace.Name)

		DeferCleanup(func() {
			By("deleting other namespace")
			Expect(testClient.Delete(ctx, nonShootNamespace)).To(Or(Succeed(), BeNotFoundError()))
		})

		By("creating relevant secret")
		secret := newRelevantSecret(resourceName, nonShootNamespace.Name)
		Expect(testClient.Create(ctx, secret)).To(Succeed())

		By("verifying secret does not get added to shootstate")
		Consistently(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).ShouldNot(containData(withName(secret.Name)))

		By("verifying secret has no finalizers")
		Consistently(func(g Gomega) []string {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			return secret.Finalizers
		}).Should(BeEmpty())
	})

	It("should not remove secrets from shootstate when shoot is in migration", func() {
		By("creating secret")
		secret := newRelevantSecret(resourceName, seedNamespace.Name)
		Expect(testClient.Create(ctx, secret)).To(Succeed())

		By("verifying secret gets synced to shootstate")
		Eventually(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).Should(containData(withName(secret.Name)))

		By("verifying secret has finalizer")
		Eventually(func(g Gomega) []string {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
			return secret.Finalizers
		}).Should(ConsistOf("gardenlet.gardener.cloud/secret-controller"))

		By("marking shoot for migration")
		shoot.Status.LastOperation = &gardencorev1beta1.LastOperation{Type: gardencorev1beta1.LastOperationTypeMigrate}
		shootRaw, err := json.Marshal(shoot)
		Expect(err).NotTo(HaveOccurred())

		By("updating cluster")
		patch := client.MergeFromWithOptions(cluster.DeepCopy(), client.MergeFromWithOptimisticLock{})
		cluster.Spec.Shoot.Raw = shootRaw
		Expect(testClient.Patch(ctx, cluster, patch)).To(Succeed())

		By("deleting secret")
		Expect(testClient.Delete(ctx, secret)).To(Succeed())

		By("verifying secret has been removed from the system")
		Eventually(func() error {
			return testClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)
		}).Should(BeNotFoundError())

		By("verifying secret info did not get removed in shootstate")
		Consistently(func(g Gomega) []gardencorev1alpha1.GardenerResourceData {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(shootState), shootState)).To(Succeed())
			return shootState.Spec.Gardener
		}).Should(containData(withName(secret.Name)))
	})
})

func containData(matchers ...gomegatypes.GomegaMatcher) gomegatypes.GomegaMatcher {
	return ContainElement(And(matchers...))
}

func withName(name string) gomegatypes.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Name": Equal(name),
	})
}

func withType(t string) gomegatypes.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Type": Equal(t),
	})
}

func withLabels(labels map[string]string) gomegatypes.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Labels": Equal(labels),
	})
}

func withData(data map[string][]byte) gomegatypes.GomegaMatcher {
	rawData, err := json.Marshal(data)
	Expect(err).NotTo(HaveOccurred())

	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Data": Equal(runtime.RawExtension{
			Raw: rawData,
		}),
	})
}

func newRelevantSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				secretsmanager.LabelKeyManagedBy:       secretsmanager.LabelValueSecretsManager,
				secretsmanager.LabelKeyManagerIdentity: "test",
				secretsmanager.LabelKeyPersist:         secretsmanager.LabelValueTrue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"some": []byte("data"),
		},
	}
}
