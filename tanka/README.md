# Tanka deployment

This directory deploys the CloudNative Supabase operator itself. It is
self-contained: the environment renders the Helm chart and CRD from this
repository and has no Jsonnet library dependencies.

The `guion` environment targets `https://kube-new.flicknote.app` and deploys
the operator into `cnsupa-system` using the public GHCR image.

```sh
make tanka-show
make tanka-diff
make tanka-apply
```

Run `make test-tanka` after changing the chart, CRD, or Jsonnet environment.
Application-specific `SupabaseProject` resources belong in their application
repositories, not in this operator environment.
