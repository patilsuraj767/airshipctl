# Secrets generation and encryption how-to-guide
## Overview of the current approach selected for secrets generation and encryption

Airshipctl consumes site manifests in order to deploy k8s cluster or update its configuration. All manifests must be stored in the SCM system: e.g. git. For security reasons this data can’t be stored in plain-text form. There are several tools that may help to handle the complexity of dealing with encrypted manifests. One of them is [Mozilla SOPS](https://github.com/mozilla/sops), which was selected to encrypt/decrypt Airshipctl manifests.

Airshipctl has a standard approach with introduction of VariableCatalogues as a configuration source and kustomize Replacement plugin which must be used to put the values to different yaml documents. Different secrets such as passwords, keys and certificates must be presented in VariableCatalogues as well. Some of them can be ‘externally provided’ - e.g. ldap credentials are typically created in some external system, e.g. Active Directory and Airshipctl just has to use them. Other secrets may be ‘internally generated’ - for example several Openstack-helm charts may want the same Openstack Keystone password and if no single external system needs that password it can be generated by Airshipctl rather than provided manually.

There can be different use-cases where the user may want instead of generating secrets to set it manually. That means that Airshipctl should allow the user to 'pin' some specific secret value rather than generate/regenerate it even though the default intent for that secret was to generate it.

Secret regeneration typically happens periodically, e.g. according to some internal policy passwords must be re-generated on yearly basis. Airshipctl should allow user to split secrets into groups that should be regenerated each period of time.

If some master key, e.g. PGP or AGE was used to encrypt secrets, some internal policies may define when this master key must be rotated. Airshipctl should allow user to easily re-encrypt the existing secrets values with new key without changing that values.

Lastly in some Treasuremap reference sites several clusters may present, e.g. ephemeral, target, lma-subcluster, wordpress-subcluster &etc. Since different people may need access to different clusters it leads to the requirement to have cluster-specific set of secrets that has to be encrypted with its own master keys and operations on secrets per cluster may be performed separately from other clusters.

This document is dedicated to the explanation of the technical details on how it’s currently done in Airshipctl and its manifests.

## Secret documents structure

Due to the need of updating parts of documents periodically the encrypted document has the following structure

``` yaml
apiVersion: airshipit.org/v1alpha1
kind: VariableCatalogue
metadata:
    labels:
        airshipit.org/deploy-k8s: "false"
    name: secrets
secretGroups:
    - name: groupName
      updated: "2021-06-07T18:01:50Z"
      values:
        - data: encryptedData...
          name: encryptedDataName
          pinned: true|false #optional
```

This structure allows to split data into groups each of them can be regenerated/updated separatelly. For that purpose it has `updated` field timestamp that is getting new value when regeneration of group is happening. Each group has an array of values. Each value has a name (should be unique in the group), data field and also optional flag `pinned`. If the value is pinned, its value isn't getting updated during regeneration. That may be helpful to flexibly switch between 'internally generated' and 'externally provided' secrets. `pinned: true` will work as 'exnternally provided'.

Airshipctl will encrypt only field `data` and that will allow to monitor all other parameters without knowing master keys for decryption.

## Secrets document location

As mentioned above there is a need in some cases to restrict access to some cluster for some people. E.g. tenant cluster manifests can be accessible to one set of users and target cluster that hosts several tenant clusters should be accessible by another people. Some people may be in both groups.

Due to that need the current manifests structure has a place for public keys that should be used to set the list of people who may decrypt that data after it was encrypted. This is defined by the set of public keys, defined in `manifests/site/test-site/<cluster>/catalogues/public-keys/kustomization.yaml` in each cluster, e.g. ephemeral, target, etc.

There is a place for private keys as well: `manifests/.private-keys/kustomization.yaml`, before work user can copy his key to my.key or to change that file to use another file. This private key will be used during data decryption in addition to the values from ENV variables that also can contain keys: SOPS_IMPORT_PGP and SOPS_IMPORT_AGE.

The Variable Catalogues with secrets can be found in `manifests/site/test-site/<cluster>/catalogues/encrypted/secrets.yaml`.
When encrypted with sops Variable Catalogue contains info who can decrypt that data - it's located in the sops field that is getting added by SOPS krm-function. SOPS krm-function used in order to encrypt and decrypt data in airship.

## SOPS krm-function overview

Airshipctl uses kustomize along with different krm-functions that extend its functionality:
* Replacement krm-function that is needed to avoid duplication of data in documents
* Templater krm-function that is needed to produce new yaml documents based on the provided parameters.

There is a standard catalog of [krm-functions](https://github.com/GoogleContainerTools/kpt-functions-catalog).
It includes the standard krm-function: `gcr.io/kpt-fn-contrib/sops` that can be used to perform decryption and encryption right in kustomize. Please refer to the [example configurations](https://github.com/GoogleContainerTools/kpt-functions-catalog/tree/master/examples/contrib/sops) that can be used to encrypt and decrypt the set of existing yamls.

Please note that to make that krm-function work it’s necessary to provide the following ENV variables:

To encrypt:
- `SOPS_IMPORT_PGP` must contain public or private key (set of keys)
- `SOPS_PGP_FP` must contain a fingerprint of the public key from the list of provided keys in `SOPS_IMPORT_PGP` that will be used for encryption.

To decrypt:
- `SOPS_IMPORT_PGP` must contain a private key (set of keys) that will be used to decrypt. Function will fail if it can’t find the key with fingerprint that was used for encryption

The gating scripts set that env variables [here](https://github.com/airshipit/airshipctl/blob/master/playbooks/airshipctl-gate-runner.yaml#L17).

## Templater krm-function use-cases overview

Templater krm-function allows users to call [Sprig functions](http://masterminds.github.io/sprig/). Sprig has a set of [functions that may generate random values, passwords, CAs, keys and certificates](http://masterminds.github.io/sprig/crypto.html). If it’s not possible to use the standard set of sprig functions for some important Airshipctl use-cases, it’s always possible to extend that set of functions: the latest version of templater krm-function introduces [extension library](https://github.com/airshipit/airshipctl/tree/master/pkg/document/plugin/templater/extlib) where this can be done. The set of already added functions can be found [here](https://github.com/airshipit/airshipctl/blob/master/pkg/document/plugin/templater/extlib/funcmap.go).

The example on how to generate different types of secrets with templater krm-function may be found [here](https://github.com/airshipit/airshipctl/tree/master/manifests/function/generate-secrets-example).

Starting Kustomize 4.0 transformer plugins are allowed to generate additional documents (before that it was prohibited by kustomize). It is also now possible to remove some of the documents in transformers. Airshipctl templater krm-function has been rebuilt to support that model as well - it now can be used in `transformers` section:
* in order to get RW access to the already existing documents that kustomize provides to templater called from `transformers` section 2 new functions were introduced: `getItems` and `setItems`.
* `getItems` and `setItems` work with [kyaml](https://github.com/kubernetes-sigs/kustomize/tree/master/kyaml/yaml) objects and because of that the additional subset of [kyaml-related functions](https://review.opendev.org/c/airship/airshipctl/+/794887/25/pkg/document/plugin/templater/extlib/funcmap.go) was introduced to manipulate kyaml-representation of documents.

Due to the requirements to encrypt different subclusters with different master keys it is necessary to have VariableCatalogue with secrets per site.

During the implementation of our working transformer it appeared that we needed go-template function feature. Templater now implements `include` function like in helm charts. Before run it scans all incoming documents and loads all functions defined in documents with apiVersion: `airshipit.org/v1alpha1` kind: `Templater`.

Essentially the set of steps that airshipctl must perform when it’s necessary to generate/regenerate/import new set of secrets is the following:

1. Load 2 already existing VariableCatalogues: with encrypted secrets and with data it's necessary to add to that encrypted VariableCatalogue (let's call it import-data)
2. Decrypt encrypted data using Sops krm-function
3. Use templater krm-function that will perform update operations. Update operations will include: merge import-data with decrypted secrets, check if some data has to be regenerated (unless it's pinned), merge regenerated data with decrypted secrets.
2. Use Sops krm-function to encrypt the yaml
3. Store the encrypted document in the document module of the site

[Secret-update phase](https://review.opendev.org/c/airship/airshipctl/+/794887/25/manifests/phases/phases.yaml) performs that steps.

The following steps are used during standard procedure or yaml rendering for other phases:
Kustomize reads the encrypted VariableCatalogue
Kustomize applies Sops transformer to decrypt the encrypted fields
This decrypted VariableCatalogue in included into the list of all catalogues and may be referenced in replacement plugin
That is done by all phases. And this set of steps is done basically this [line](https://github.com/airshipit/airshipctl/blob/master/manifests/site/test-site/target/catalogues/kustomization.yaml#L7).

Let’s show how this feature works under the hood.

## GenericContainer feature overview

In order to implement all that functionality it was necessary to introduce a new feature called GenericContainer. It’s a type of Executor (see [this document](https://github.com/airshipit/airshipctl/blob/master/docs/source/phases.rst)) and it allows to run Container with a document bundle as input. Krm-function can be run by GenericContainer. It may be that we may add some other types of API with containers.

Krm-functions accept a set of yamls and config as input and return a modified set of yamls.
GenericContainer executor may just output it to stdout. Or it may store it as `kpt fn sink` does.
In particular we’re using the second option to store our generated and encrypted yamls to the specific places from which other manifests will take [ephemeral secrets file](manifests/site/test-site/ephemeral/catalogues/encrypted/secrets.yaml) or [target secrets file](manifests/site/test-site/target/catalogues/encrypted/secrets.yaml).

As an example it’s possible to see [target kustomization](manifests/site/test-site/target/catalogues/encrypted/kustomization.yaml) performs decryption using sops krm-function.

# Step-by-step Operator instructions

## Manual operations that are automated by Airshipctl related to generation and encryption

### Password generation

Airshipctl has variety of options to generate passwords:
1. derivePassword from Sprig library
2. [regexGen](https://github.com/airshipit/airshipctl/blob/master/pkg/document/plugin/templater/extlib/regexgen.go#L22)

Without automation it would be necessary to generate passwords based on the polices manually (with some external tools).

### Key/CA/Cert Generation

K8s requires the user to have a key/cert in order to be able to authenticate. Without Airshipctl it would be necessary to use openssl tool and run several commands in right order to generate key/CA, and after that to generate user key, create CSR and get user certificate signed with this CA, e.g.

``` sh
# getting CA key/crt  -> tls.key/tls.crt
openssl req -x509 -subj "/CN=Kubernetes API" -new -newkey rsa:2048 -nodes -keyout tls.key -sha256 -days 3650 -out tls.crt
# generating admin’s key and creating CSR
openssl req -subj "/CN=admin/O=system:masters" -new -newkey rsa:2048 -nodes -out admin.csr -keyout admin.key -out admin.csr
# signing CSR with CA and getting admin’s cert
openssl x509 -req -in admin.csr -CA tls.crt -CAkey tls.key -CAcreateserial -out admin.crt -days 365 -sha256
```

After that it would be necessary to convert it to base64 form and put it in the right place in the model, which is very tiresome and error-prone operation especially along generation.

Airshipctl allows to do the same with running one phase to generate and encrypt all secrets.

### Secrets encryption with Mozilla SOPS

SOPS is a very powerful tool for encryption of different documents. Check its site to get all possible ways to use it.
In this document we’re going to describe only the way how it’s already used.

If you have a VariableCatalogue that contains secrets and you want to encrypt it with SOPS using PGP, you’ll need to generate a pair of keys (public and private). Public will be used for encryption and Private for description.

The easiest way to generate sops keys - to use gpg wizard:

``` sh
gpg --full-generate-key
```

If it should be done by automation there are some other command options that allow you to specify params instead of interactive mode. E.g. please refer to this [link](https://docs.github.com/en/github/authenticating-to-github/generating-a-new-gpg-key).

In the Airshipctl gate we’re using SOPs pre-generated keys from [here](https://github.com/mozilla/sops/blob/master/pgp/sops_functional_tests_key.asc). They can be imported as it’s demonstrated [here](https://github.com/mozilla/sops#test-with-the-dev-pgp-key).
To encrypt file with 1 yaml it’s necessary to use the command:

```
SOPS_PGP_FP='FBC7B9E2A4F9289AC0C1D4843D16CEE4A27381B4' sops -e <file name>
```

`SOPS_PGP_FP` must contain a fingerprint of one of the public keys that gpg already has on the local machine.

**Note:** Sops allows you to set several fingerprints. During decryption it’s possible to have only one of them.

To decrypt file manually (the private key has to be present) just simply do:

```
sops -d <file name>
```

We hope that it won’t be necessary to do these actions manually, because airshipctl already has automation that does encryption of generated secrets and decryption of secrets that will be used by manifests right during phase execution.

**But It’s good to know one very useful command (that requires private key to be imported to gpg)**

```
sops <file name>
```

This will decrypt the file and will open it in the editor. It will be possible to perform needed modifications. Once finished just close the editor and sops will encrypt the modified document and put it back. This may be a really-really useful command for some users and very simple at the same time. This approach may be used in order to modify [imported secrets](manifests/site/test-site/target/encrypted/results/imported/secrets.yaml).

## Generation/Regeneration and encryption of secrets in manifests

Now when we have all the information about what is going on under the hood, let’s see how Airshipctl automats generation and encryption.

Note: This section will require the reader to understand how kustomize works in very good details.
The good start will be the official documentation, but that may not be enough.
Here are some documents that were created during design of airshipctl:

1. [Reusable kustomize modules](https://docs.google.com/presentation/d/1gCAIsETGFYjVim0ChEQHmDwTtrJWbcFiBbFGDN7gQXA/edit#slide=id.g1f87997393_0_782)
2. [Kustomize evolution](https://docs.google.com/presentation/d/1RnH41i-1sQRfE4G0c-xvN4Xl5dZgCL3SwM3BiWk42-o/edit)
3. [Kustomize evolution - Video about krm-functions in Airshipctl](https://drive.google.com/file/d/16mk2JjnSYwmDhZSff77K_qJdv_zUK9fX/view?usp=sharing)
4. [Documentation about Airshipctl Phases](https://github.com/airshipit/airshipctl/blob/master/docs/source/phases.rst)

Now let’s refer to the way how the current version of manifests works for gating.
Let’s start from the secrets generator.

To run it it’s just necessary to run the phase:

```
airshipctl phase run secret-update
```

This phase accepts parameters via env variables:
* `FORCE_REGENERATE` - accepts a comma-separated list of periods that must be regenerated, e.g. yearly,monthly
* `ONLY_CLUSTERS` - accepts a comma-separated list of clusters inside site that must be regenerated. This is helpful when the user has keys only for 1 subcluster and wants to perform update operation only for its secrets
* `TOLERATE_DECRYPTION_FAILURES` - should be `true` if `ONLY_CLUSTERS` option is used.

The following command is done each time we run integration testing in CI in this [file](tools/deployment/23_generate_secrets.sh) to regenerate all groups:

```
FORCE_REGENERATE=all airshipctl phase run secret-update
```

This commands updates all secrets in the following locations `ephemeral/catalogues/encrypted/secrets.yaml` and `target/catalogues/encrypted/secrets.yaml`. Here is the way how it works:
* it gets already decrypted documents by taking kustomization results from `encrypted/get/kustomization.yaml`.
* it also import-data encrypted/update/secrets.yaml. This file contains diff user wants to apply to the encrypted data.
* it executes templater-based transformer from manifests/type/gating/shared/update-secrets/template.yaml and it performs all magic (see below). as a result it produces unencrypted updated secrets catalogues, cleans up import-data and sets `config.kubernetes.io/path` annotations (see below) so the files can be stored by airshipctl to the right location.
* the resulting bundle is encrypted by genericContainer executor and getting stored by the location set in `config.kubernetes.io/path` annotations.

Let's look closer into the [templater](manifests/type/gating/shared/update-secrets/template.yaml) that does the whole job on generation. It can be redefined for different site types to incorporate templates for subclusters.

The template contains definition of functions that define how to generate each section of secrets, e.g.

```
  {{- define "regenEphemeralK8sSecrets" -}}
    {{- $ClusterCa := genCAEx .ephemeralCluster.ca.subj (int .ephemeralCluster.ca.validity) }}
    {{- $KubeconfigCert := genSignedCertEx .ephemeralCluster.kubeconfigCert.subj nil nil (int .ephemeralCluster.kubeconfigCert.validity) $ClusterCa -}}
  values:
    - data: {{ $ClusterCa.Cert | b64enc | quote }}
      name: caCrt
    - data: {{ $ClusterCa.Key | b64enc | quote }}
      name: caKey
    - data: {{ $KubeconfigCert.Cert | b64enc | quote }}
      name: crt
    - data: {{ $KubeconfigCert.Key | b64enc | quote }}
      name: key
  {{- end -}}
```

It also contains the code that finds the document with secrets and document with imports for that particular subcluster. E.g. for ephemeral subcluster it's:

```
  {{/* get combined-secrets yaml and exclude it from the bundle */}}
  {{- $combinedSecrets := index (KOneFilter getItems (include "grepTpl" (list "[\"metadata\", \"name\"]" "^combined-ephemeral-secrets$" "false"))) 0 -}}
  {{- $_ := setItems (KOneFilter getItems (include "grepTpl" (list "[\"metadata\", \"name\"]" "^combined-ephemeral-secrets$" "true"))) -}}
  {{/* get combined-secrets-import yaml and exclude it from the bundle */}}
  {{- $combinedSecretsImport := index (KOneFilter getItems (include "grepTpl" (list "[\"metadata\", \"name\"]" "^combined-ephemeral-secrets-import$"))) 0 -}}
```

As we can see some inbuilt kyaml functions are used for that purpose, e.g. `KOneFilter` - it applies the filter defined in the second parameter to the input bundle taken by `getItems` function. The filter ensures that in the resulting documents ther will be documents that have `metadata.name == combined-ephemeral-secrets`. Also we see that the filter is getting generated by go-template function called `grepTpl`. It's stored in go-template module, its implementation can be found [here](manifests/function/templater-helpers/secret-generator/lib.yaml). SetItems is used to exclude found documents from bundle - because this template add its own document with the same name, that contains all merged/regenerated data. We see that below:

```
  apiVersion: airshipit.org/v1alpha1
  kind: VariableCatalogue
  metadata:
    annotations:
      config.kubernetes.io/path: "ephemeral/catalogues/encrypted/secrets.yaml"
    labels:
      airshipit.org/deploy-k8s: "false"
    name: combined-ephemeral-secrets
  secretGroups:
    - {{ include "group" (list . $combinedSecrets $combinedSecretsImport "isoImageSecrets" "once" "regenIsoImageSecrets" ) | indent 4 | trim }}
    - {{ include "group" (list . $combinedSecrets $combinedSecretsImport "ephemeralK8sSecrets" "once" "regenEphemeralK8sSecrets" ) | indent 4 | trim }}
```

We see that the body of groups are generated by the go-template function `group` that takes care of mering previous values of secrets with data from imports as well as about regeneration of data when needed by calling another function provided as the last parameter. The implementation of this function can be found [here](manifests/function/templater-helpers/secret-generator/lib.yaml).

Now if we refer back to the Phase description we’ll see that it’s type is GenericContainer with the name `encrypter`.

The definition of that executor is the following:

```apiVersion: airshipit.org/v1alpha1
kind: GenericContainer
metadata:
  name: encrypter
  labels:
    airshipit.org/deploy-k8s: "false"
spec:
  type: krm
  sinkOutputDir: "./"
  image: gcr.io/kpt-fn-contrib/sops:v0.3.0
  envVars:
    - SOPS_IMPORT_PGP
    - SOPS_PGP_FP
config: |
  apiVersion: v1
  kind: ConfigMap
  data:
    cmd: encrypt
    cmd-json-path-filter: '$[?(@.metadata.name=="combined-ephemeral-secrets" || @.metadata.name=="combined-target-secrets")]'
    encrypted-regex: '^(data)$'
```

Basically this executor accepts the bundle, runs krm-function `gcr.io/kpt-fn-contrib/sops:v0.3.0` with configuration from `config` field and stores the result to the directory `./`(root directory of the current site) based on the filenames/hierarchy defined by annotation `config.kubernetes.io/path`. Sops krm-function in its turn encrypts documents and that means that `target/generator/results/` will contain encrypted yamls. To make that work the user will need just to specify 2 additional environment variables:

- `SOPS_IMPORT_PGP`
- `SOPS_PGP_FP`

Combination of different parameters provided via env variables can be used in different situations. For instance that allows to regenerate everything, regenerate only some secrets, regenerate only secrets for one subcluster, reencrypt only one subcluster without regeneration and etc. Some examples may be found [here](tools/deployment/23_generate_secrets.sh) as sanity tests.

## Decryption of secrets and using them

The current implementation of manifests doesn’t require explicit decryption of files. All secrets are decrypted on the spot. Here are the details of how it was achieved:
Cluster encrypted documents are listed in its catalogue, e.g. [target secrets](manifests/site/test-site/target/catalogues/encrypted/secrets.yaml).
[The kustomization file](manifests/site/test-site/target/catalogues/encrypted/kustomization.yaml) performs decryption by invoking `decrypt-secrets` transformer, that is just a sops krm-function configuration that decrypts all encrypted documents.
Note: we made a special kustomization for decrypt-secrets configuration just to be able to modify it a bit depending on the environment variable `TOLERATE_DECRYPTION_FAILURES` value. If it’s true we’re adding parameter `cmd-tolerate-failures: true` to sops configuration.

Once decrypted that VariableCatalogues may be imported as well as other catalogues. E.g.:
See [this line in the kustomization file](manifests/site/test-site/target/catalogues/kustomization.yaml#L7).
And it’s possible to use their values as a source for replacement transformer. E.g. [this replacement plugin configuration](manifests/site/test-site/kubeconfig/update.yaml) updates fields of kubeconfig in order to put there generated keys/certs.

To get even more familiar with that approach and understand all details please refer to the [following commit] (https://github.com/airshipit/airshipctl/commit/a252b248bcc9be2c8aca6f544f99541dce5012a3).

## Decryption and printing the generated secrets to the screen

In some cases it may be necessary to see what was generated by the templater in unencrypted form. For example, new SSH-keys were generated and it's necessary to get
the private in order to be able to login to the node. Since in general it maybe very useful another phase called `secret-show` has been introduced.
It decrypts and prints out the generated secrets.

## Master key rotation

This procedure may be done in many different ways depending on the organizational processes.
There are 2 different approaches that may be used:

1. when we create a new key - all secrets are getting re-encrypted with that new key
2. when we create a new key - we're using it for generation/encryption of new secrets, but the old one stays valid till the last secret encrypted with it is getting regenerated and encrypted with new one. That means that old and new keys are used for decryption in parallel during some 'overlap' period. This is be similar to the approach that [Sealed secrets project](https://github.com/bitnami-labs/sealed-secrets) selected.

Both approaches are possible taking into account that fact that SOPS allows you to have several private keys to decrypt data and it selects the needed one automatically.

Nevertheless for the sake of simplicity we're currently implemented the first approach in our manifests. There is a phase called `secret-update` that allows to perform master key rotation.

In order to do so please follow the following steps:

1. generate new master key pair using, e.g. using gpg wizard:

``` sh
gpg --full-generate-key

```
Note: please make sure you know the fingerprint of the newly generated key.

2. append the env variable `SOPS_IMPORT_PGP` with the new keypair (don't delete the previous one at this step, because it's needed for decryption).
3. set the env variable `SOPS_PGP_FP` to the value of the NEW private key fingerprint. That means that the new key will be used for encryption.
4. run `airshipctl phase run secret-update`. make sure it runs successfully.
5. check that all encrypted files were updated and that pgp.fp field for all of them equal to the value you specified in `SOPS_PGP_FP`.
6. now it's possible to delete the old master key from `SOPS_IMPORT_PGP`. Once done it's possible to run `airshipctl phase run secret-show` to ensure that the keys will be decrypted properly.
8. commit the changes to the site manifests.

# Troubleshooting typical cases

Note: In order to make troubleshooting possible please set env variable `DEBUG_SOPS_GPG=true` to see all debug output.

## Validate keys fingerprints

Sops function fails with the following typical output:

```
gpg: keybox '/tmp/pubring.kbx' created
gpg: /tmp/trustdb.gpg: trustdb created
gpg: key D8720D957C3D3074: public key "SOPS Functional Tests Key 2 (https://github.com/mozilla/sops/) <secops@mozilla.com>" imported
gpg: key D8720D957C3D3074: secret key imported
gpg: key 3D16CEE4A27381B4: public key "SOPS Functional Tests Key 1 (https://github.com/mozilla/sops/) <secops@mozilla.com>" imported
gpg: key D8720D957C3D3074: "SOPS Functional Tests Key 2 (https://github.com/mozilla/sops/) <secops@mozilla.com>" not changed
gpg: key 19F9B5DAEA91FF86: public key "SOPS Functional Tests Key 3 (https://github.com/mozilla/sops/) <secops@mozilla.com>" imported
gpg: Total number processed: 4
gpg:               imported: 3
gpg:              unchanged: 1
gpg:       secret keys read: 1
gpg:   secret keys imported: 1
[ERROR] Sops command results in error for
apiVersion: airshipit.org/v1alpha1
kind: VariableCatalogue
metadata:
  labels:
    airshipit.org/deploy-k8s: 'false'
  name: password-secret
passwordRandom1: 'ENC[AES256_GCM,data:o1xUrKiOPaucB+U2JSg=,iv:vJkmHG5B9/xiQA+qfRHyYwQFKIG1P0S0k8qwFCEyICk=,tag:MqLeMZ3BXhNKaUKvZoLStw==,type:str]'
sops:
  azure_kv: []
  gcp_kms: []
  hc_vault: []
  kms: []
  lastmodified: '2021-01-14T11:23:10Z'
  mac: 'ENC[AES256_GCM,data:7aMFeEfn5MXU9M7U+rQ7fIcWG6A6BZILsvgVyEl+esa8EhEsOL6dRfITq2x+1t6ft+H5nRqbO5GyXJ3mhu7n/x5FBVVqBcZrvydojrqBWizXA4HQAc3t8OS3D1I2WLLx+S7mI5AiKDERGZX4ImiahSebqL/bNfpYdDQP+gX8+vQ=,iv:zchumZaGhTpyEEsJMMlW/e1vieqjVKT32Kiv0LuLPlk=,tag:q0vWzGZ8D4HYHTvdRymG0g==,type:str]'
  pgp:
  - created_at: '2021-01-14T11:23:10Z'
    enc: |
      -----BEGIN PGP MESSAGE-----

      hQEMAyUpShfNkFB/AQf+IIXYumKkSmzMHCoJVXculVowkez4aUI/OpdNw2CPWNDd
      3Kzea6kTv64ef+kll9DhczP0gVlgUZ0p0MenBfmkI4qt3wr5fyRUVjUpfF/R8Gmc
      9GZf4myDD5T2wDJVCkNmO2wogbZ7IZaGdx0HV3DihvSGg0xcGBUaFp/zeR9vXTQs
      a+CecTBm4+7uLnDvHf4Rathy3gnlLrLLdsJXRgEOJ2Fqp/JjoqFqsWOol9lFwALM
      yRkxbWjeL7ePddXBZ8QmOB/AB0RKSRQ2Yd9RXpp1gSFKn5NOfWIZsaVgdds2zOw5
      R5syWHhfzVylAxNrKJYIgr9hLje48W/Y6GSezkGvG9JcAebQzVP53UtXkwJSIjda
      86WAFwpgpZ0sEG7zpSpxS8p4g3XsXjOdD2b0y/dwXGYK5oeOjb/wGYFf1EX0p0xk
      BqGQ8JHxikqW8oEuyEgeg96uEMZb1Vy7u657zPw=
      =VfIN
      -----END PGP MESSAGE-----
    fp: FBC7B9E2A4F9289AC0C1D4843D16CEE4A27381B4
  unencrypted_regex: ^(kind|apiVersion|group|metadata)$
  version: 3.6.1

 Failed to get the data key required to decrypt the SOPS file.

Group 0: FAILED
  FBC7B9E2A4F9289AC0C1D4843D16CEE4A27381B4: FAILED
    - | could not decrypt data key with PGP key:
      | golang.org/x/crypto/openpgp error: Could not load secring:
      | open /tmp/secring.gpg: no such file or directory; GPG binary
      | error: exit status 2

Recovery failed because no master key was able to decrypt the file. In
order for SOPS to recover the file, at least one key has to be successful,
but none were.
```

It’s necessary to pay attention to the first part of the message - where gpg performs key import and compares it with the fingerprint of the key in the encrypted document itself. The fingerprint that was used is (from sops.pgp.fp):

```
fp: FBC7B9E2A4F9289AC0C1D4843D16CEE4A27381B4
```

In the sops krm function logs it’s possible to see that the following public keys are imported:
D8720D957C3D3074
3D16CEE4A27381B4
D8720D957C3D3074 (second time :) )
19F9B5DAEA91FF86
And only 1 private key is imported:
D8720D957C3D3074

Pgp shows only last 16 symbols of fingerprint. Our fingerprint's last 16 symbols are: `3D16CEE4A27381B4`.
It’s clear that the imported bundle with public and private key didn’t have a private key with this fingerprint. That was the reason why sops wasn’t able to decrypt the document. The solution is - to make sure that all needed keys are imported.

## Validate format of the encrypted message

UPD:
the root-cause of that behavior was identified [here](https://github.com/airshipit/airshipctl/issues/471).

Here is another typical output:

```
...
sops:
  azure_kv: []
  gcp_kms: []
  hc_vault: []
  kms: []
  lastmodified: '2021-02-12T17:01:46Z'
  mac: 'ENC[AES256_GCM,data:JeDU6fOEC1Yz5vIWS5A9TJfqC3SpVds+96F27fH7UXOLxLzSEQjCbQFXdZzW3FEJvFPrPz8KcnWsm1VZQJRZIpyJCNhJfpq302CadUsohs4kLVbgoSHkXWtTVYLSQn6BmjfSaMeghJAQ6LgqE7AtgpSnc5d+F8kuRDr/AQE0Nv0=,iv:T4dzbWQFSzPAZjEevch79MTjKmsd1Ia4t5xiY5+ZAVw=,tag:Zf5XM4OieHGXx4s/3UO2Tw==,type:str]'
  pgp:
  - created_at: '2021-02-12T17:01:44Z'
    enc: |-
      -----BEGIN PGP MESSAGE-----
      wcBMAyUpShfNkFB/AQgAibVYA6Cu3LcZ0/Q//4DRpUnVQ8iRUfTBAzDihvE36hFt
      haKwbA/zwdivwNpVCdyw0qoAGwMrXlaSFhsrpdXDNV1dPqVoOzRd5EBIl13xbQGP
      hqR4c4BKIkJM4hGO3LpNNLi6cR9lMmUi06TGVp2GkO8aCVmbTK6Q8RdHRtKisxfb
      pEpiMl9vpequ2IgnWhd+XSy6rCMWpldLzqT1dBMSjSON0TBtLOXB2gqWaszGNhDs
      pfuYo1F0xO86HblgOURTLJ+lr0rhPMn55iiNL1JG5hQcj0to4UKTCKCpOZZrAk0n
      MbfrwIRDC9Nd5xVjl/TNA1IQN9DAapHYWMMHsl3LOdLgAeR0uJnuZ5rwHEokMits
      zhxV4RHG4BfgjOHP5uBw4ubrJxjgHeXL4YBiGY+oPNtaLRI+xzXtw7uT27pwa0ww
      7PFJ3SGcZeCV5ImYQXqdwF70smk47EMHNUbijA5WNeEpkQA=
      =Zv9X
      -----END PGP MESSAGE-----
    fp: FBC7B9E2A4F9289AC0C1D4843D16CEE4A27381B4
  unencrypted_regex: ^(kind|apiVersion|group|metadata)$
  version: 3.6.1
…

 Failed to get the data key required to decrypt the SOPS file.

Group 0: FAILED
  FBC7B9E2A4F9289AC0C1D4843D16CEE4A27381B4: FAILED
    - | could not decrypt data key with PGP key:
      | golang.org/x/crypto/openpgp error: Could not load secring:
      | open /tmp/secring.gpg: no such file or directory; GPG binary
      | error: exit status 2

Recovery failed because no master key was able to decrypt the file. In
order for SOPS to recover the file, at least one key has to be successful,
but none were.
```

In fact this is a pretty rare issue, but still I saw that once.
The problem is the following part

```

enc: |-
      -----BEGIN PGP MESSAGE-----
      wcBMAyUpShfNkFB/AQgAibVYA6Cu3LcZ0/Q//4DRpUnVQ8iRUfTBAzDihvE36hFt
```

should have one empty line after `-----BEGIN PGP MESSAGE-----`. It should look like this:

```
enc: |-
      -----BEGIN PGP MESSAGE-----

      wcBMAyUpShfNkFB/AQgAibVYA6Cu3LcZ0/Q//4DRpUnVQ8iRUfTBAzDihvE36hFt
```
Sops typically starts working after that correction.
