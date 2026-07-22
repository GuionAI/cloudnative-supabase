local namespace = 'cnsupa-system';
local imageTag = std.extVar('imageTag');
local chart = std.native('helmTemplate')(
  'cloudnative-supabase',
  '../../../charts/cloudnative-supabase',
  {
    calledFrom: std.thisFile,
    namespace: namespace,
    values: {
      versionOverride: imageTag,
      image: {
        tag: imageTag,
        pullPolicy: 'IfNotPresent',
      },
    },
  },
);

{
  namespace: {
    apiVersion: 'v1',
    kind: 'Namespace',
    metadata: {
      name: namespace,
      labels: {
        'app.kubernetes.io/name': 'cloudnative-supabase',
        'app.kubernetes.io/managed-by': 'tanka',
      },
    },
  },
} + chart
