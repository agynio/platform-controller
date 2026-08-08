# Platform Controller

Reconciles what a release declares the platform should contain against the
platform's own API.

Some resources must exist for a freshly installed platform to be usable at all:
an organization for the platform's own resources to live in, the images it
ships, the runner that executes agent workloads, the apps bundled with the
release, and at least one cluster administrator. None can be created the
ordinary way at install time — the ordinary way requires a signed-in user with
organization ownership, and at install time there is no user and no
organization.

This closes that gap declaratively. A release declares those resources as
Kubernetes objects; this controller reconciles them through the same
[Gateway](https://github.com/agynio/gateway) methods the Console calls,
authenticating as the platform admin identity. There is no install script and no
ordered sequence of calls: an object that cannot be reconciled yet is retried
until it can be.

See [Platform Resource Provisioning](https://github.com/agynio/architecture/blob/main/architecture/operations/platform-provisioning.md).

## Kinds

`platform.agyn.io/v1alpha1`, all namespaced to the release:

| Kind | Reconciled against | Removal |
|---|---|---|
| `Organization` | `CreateOrganization` | orphaned |
| `ClusterAdmin` | the [Users](https://github.com/agynio/users) cluster role | **revoked** |
| `Image` | `CreateImage`, public | orphaned |
| `Runner` | `RegisterRunner`, cluster-scoped | orphaned |
| `App` | `CreateApp` | orphaned |
| `OverlayPolicy` | the OpenZiti controller, via Ziti Management | orphaned |

Removing a declaration is not a request to destroy data — deleting an app would
take everything it owns with it. A cluster admin grant is the exception, because
an unrevokable grant is a hole rather than a resource.

## Behaviour

- **No ordering.** Every precondition becomes true at a moment no chart can
  predict, so nothing is sequenced. A failed call is a retry, not an error.
- **The declaration is authoritative.** A change made directly against the
  platform API is corrected on the next pass. This is what lets a release change
  a resource it shipped earlier.
- **Progress is per-object.** What was created is recorded on each object's
  status, and every object carries conditions saying whether it is reconciled,
  pending, or failing and why. A platform that installed without provisioning is
  visible as objects that are not Ready.
- **Service tokens are written once.** Registering a runner or app returns its
  token once, so its Secret is the only copy. A lost Secret is reported as
  `CredentialMissing` rather than silently re-registering; recovery is deleting
  the declaration so it is recreated.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `GATEWAY_URL` | `http://gateway:8080` | The ordinary API |
| `BOOTSTRAP_TOKEN_FILE` | `/etc/agyn/bootstrap/token` | The platform admin identity's credential, mounted from a Secret |
| `ZITI_MANAGEMENT_TARGET` | `http://ziti-management:50051` | Overlay policies only |
| `WATCH_NAMESPACE` | — | Required; where declarations and their Secrets live |

## Development

```bash
buf generate buf.build/agynio/api   # .gen/ is not committed
go test ./...
```

Regenerate the CRDs and deepcopy after changing a type:

```bash
go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5 object paths=./api/...
go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5 crd paths=./api/... output:crd:artifacts:config=charts/platform-controller/crds
```

The generated definitions are committed: they ship in `charts/platform-controller/crds`
for a first install, and the chart's `pre-upgrade` hook applies the same files
server-side so they upgrade in place with the release. A definition is never
deleted in order to be replaced — it owns every object of its kind.
