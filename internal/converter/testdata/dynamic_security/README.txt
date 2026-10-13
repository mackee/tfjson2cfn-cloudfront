This is a standalone, offline regression fixture with dummy credentials and
example.test domains. Generate it using Terraform 1.15.6 and AWS provider 6.50.0:

  terraform init
  terraform plan -refresh=false -out=plan.bin
  terraform show -json plan.bin > plan.json

Remove the volatile top-level timestamp and format JSON with sorted keys.
No apply or AWS authentication is required. The policies are new resources,
so their IDs are unknown. Dynamic ordered block expressions are absent from
the configuration JSON. Tests supply explicit resource-instance references.

The separate unknown_instance_security.json fixture is synthetic and exercises
known managed signer/OAC IDs in combination with unknown instance policy IDs.
