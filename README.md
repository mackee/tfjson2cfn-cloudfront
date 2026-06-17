# tfjson2cfn-cloudfront

**Convert a Terraform plan into a CloudFront-only CloudFormation template — the Terraform on-ramp for [localfront](https://github.com/mackee/localfront).**

`tfjson2cfn-cloudfront` reads the JSON that `terraform show -json` emits for a plan and writes a CloudFormation template containing just the `AWS::CloudFront::*` resources. Because Terraform has already resolved every HCL expression — variables, functions, `for_each`, data sources, module wiring — the converter never interprets HCL itself: it reshapes already-known values into CloudFormation's schema and restores cross-resource links as `Ref` / `Fn::GetAtt`. The resulting template is what you feed to localfront, so Terraform users get the same instant local CloudFront emulation that CloudFormation and CDK users already have.

> **Status: Proof of Concept.** This README defines the intended scope and CLI. Behavior, flags, and output may change without notice.

## Why

localfront is driven entirely by CloudFormation templates — it has no management API and reads no Terraform. CDK users `cdk synth`; CloudFormation users point it at their template directly. Terraform users had no on-ramp.

A general-purpose Terraform→CloudFormation converter is intractable: it would have to re-implement HCL evaluation, every provider schema, and the entire resource catalog. Two constraints make *this* one tractable:

1. **Consume the plan, not the HCL.** `terraform show -json <planfile>` is fully resolved JSON. Variables, `for_each`, `locals`, data sources, and interpolation are already collapsed to concrete values by Terraform itself.
2. **Scope to CloudFront.** Only `aws_cloudfront_*` resources are translated — exactly the resource types localfront understands. Everything else in the plan is ignored.

What's left is a focused schema translation plus reference resolution.

## How it works

```
terraform plan -out plan.tfplan
terraform show -json plan.tfplan   ─┐
                                    ├─►  tfjson2cfn-cloudfront  ─►  template.yaml  ─►  localfront serve
        (resolved plan JSON)       ─┘        (this project)         (CFN template)
```

1. Read the plan JSON (stdin or `--input`).
2. For each `aws_cloudfront_*` resource, emit the matching `AWS::CloudFront::*` resource, mapping Terraform's snake_case blocks to CloudFormation's PascalCase properties.
3. Where an attribute references another resource in the same plan, emit a `Ref` / `Fn::GetAtt` instead of a literal value.
4. Write a CloudFormation template (YAML by default, JSON optional).

## Quick start

> The CLI shown below is the planned interface; flags may change.

```console
$ go install github.com/mackee/tfjson2cfn-cloudfront/cmd/tfjson2cfn-cloudfront@latest

$ terraform plan -out plan.tfplan
$ terraform show -json plan.tfplan | tfjson2cfn-cloudfront > template.yaml

$ localfront serve --template template.yaml \
    --public-host assets.example.test:8080 \
    --s3-endpoint http://localhost:9000 ...
```

Or with explicit files:

```console
$ terraform show -json plan.tfplan > plan.json
$ tfjson2cfn-cloudfront --input plan.json --output template.yaml --format yaml
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-i, --input` | `-` (stdin) | Path to `terraform show -json` output |
| `-o, --output` | `-` (stdout) | Path to write the CloudFormation template |
| `--format` | `yaml` | Output format: `yaml` or `json` |
| `--log-level` | `info` | `debug\|info\|warn\|error` (written to stderr) |

The converter reads JSON on stdin and writes the template on stdout by default, so it drops straight into a `terraform show … | tfjson2cfn-cloudfront | …` pipeline. Diagnostics go to stderr, keeping stdout clean for the template consumer.

## Supported resources

Each Terraform CloudFront resource maps to the CloudFormation type localfront supports:

| Terraform | CloudFormation | Status |
| --- | --- | --- |
| `aws_cloudfront_distribution` | `AWS::CloudFront::Distribution` | ✅ implemented |
| `aws_cloudfront_function` | `AWS::CloudFront::Function` | ✅ implemented |
| `aws_cloudfront_key_value_store` | `AWS::CloudFront::KeyValueStore` | ✅ implemented |
| `aws_cloudfront_cache_policy` | `AWS::CloudFront::CachePolicy` | planned |
| `aws_cloudfront_origin_request_policy` | `AWS::CloudFront::OriginRequestPolicy` | planned |
| `aws_cloudfront_response_headers_policy` | `AWS::CloudFront::ResponseHeadersPolicy` | planned |
| `aws_cloudfront_public_key` | `AWS::CloudFront::PublicKey` | planned |
| `aws_cloudfront_key_group` | `AWS::CloudFront::KeyGroup` | planned |
| `aws_cloudfront_origin_access_control` | `AWS::CloudFront::OriginAccessControl` | planned |

The three implemented types cover the `examples/`, which are golden-tested against the templates [localfront](https://github.com/mackee/localfront) ships. References *to* the planned types (a distribution's `cache_policy_id`, `trusted_key_groups`, `origin_access_control_id`, …) already resolve to `Ref`; only emitting the policy resources themselves is pending. A `planned` CloudFront type in the plan is skipped with a warning rather than translated; any non-CloudFront resource (`aws_s3_bucket`, `aws_iam_*`, …) is ignored silently.

### Schema translation

Terraform and CloudFormation describe the same CloudFront concepts with different shapes; the converter knows both:

- Repeated blocks become arrays: `origin { … }` → `Origins: [ … ]`, `ordered_cache_behavior { … }` → `CacheBehaviors: [ … ]`, `custom_error_response { … }` → `CustomErrorResponses: [ … ]`.
- The flat `aws_cloudfront_distribution` body is nested under `DistributionConfig` (and likewise `CachePolicyConfig`, `OriginRequestPolicyConfig`, … for the policy resources).
- snake_case → PascalCase, with CloudFront's naming quirks (`is_ipv6_enabled` → `IPV6Enabled`, `viewer_certificate` → `ViewerCertificate`, …).
- Legacy `forwarded_values` is carried over as-is (localfront accepts it).

### Reference resolution

Inside a plan, an attribute that points at another resource — e.g. `cache_policy_id = aws_cloudfront_cache_policy.assets.id` — is "known after apply" and so is absent from the resolved values. The converter detects the dependency from the plan's `configuration` section and restores it as a CloudFormation intrinsic against the converted logical ID:

| Terraform reference | Emitted |
| --- | --- |
| `cache_policy_id = aws_cloudfront_cache_policy.x.id` | `CachePolicyId: !Ref X` |
| `function_arn = aws_cloudfront_function.x.arn` | `FunctionARN: !GetAtt X.FunctionARN` |
| `trusted_key_groups = [aws_cloudfront_key_group.x.id]` | `TrustedKeyGroups: [ !Ref X ]` |
| `key_value_store_arn = aws_cloudfront_key_value_store.x.arn` | `KeyValueStoreARN: !GetAtt X.Arn` |

Managed policy IDs (`Managed-CachingOptimized`, …) and other literal values are passed through unchanged — localfront resolves those itself.

Logical IDs are derived deterministically from the Terraform resource address (`aws_cloudfront_distribution.assets` → `AwsCloudfrontDistributionAssets`), so the output is stable across runs.

## Not supported / out of scope

- **Non-CloudFront resources** — only `aws_cloudfront_*` is translated, by design.
- **HCL evaluation** — the converter reads resolved plan JSON; it never parses `.tf` files. If a value can't be resolved by `terraform plan` (e.g. it depends on the apply-time output of a non-CloudFront resource), it stays unknown and that property is omitted with a warning.
- **Round-tripping back to AWS** — output is one-way, intended for localfront, not for `aws cloudformation deploy`.
- **Property-level fidelity for what localfront ignores** — properties are still translated when present, but see localfront's *Accepted but ignored* / *Not implemented* lists for what actually takes effect at serve time.

## Example

```hcl
resource "aws_cloudfront_cache_policy" "assets" {
  name        = "assets"
  default_ttl = 86400
  # …
}

resource "aws_cloudfront_distribution" "assets" {
  enabled             = true
  aliases             = ["assets.example.test"]
  default_root_object = "index.html"

  origin {
    origin_id   = "s3"
    domain_name = "assets.s3.us-east-1.amazonaws.com"
  }

  default_cache_behavior {
    target_origin_id       = "s3"
    viewer_protocol_policy = "allow-all"
    cache_policy_id        = aws_cloudfront_cache_policy.assets.id
  }

  custom_error_response {
    error_code         = 404
    response_code      = 200
    response_page_path = "/index.html"
  }
  # restrictions / viewer_certificate omitted for brevity
}
```

```console
$ terraform show -json plan.tfplan | tfjson2cfn-cloudfront
```

```yaml
Resources:
  AwsCloudfrontCachePolicyAssets:
    Type: AWS::CloudFront::CachePolicy
    Properties:
      CachePolicyConfig:
        Name: assets
        DefaultTTL: 86400
        # …
  AwsCloudfrontDistributionAssets:
    Type: AWS::CloudFront::Distribution
    Properties:
      DistributionConfig:
        Enabled: true
        DefaultRootObject: index.html
        Aliases: [assets.example.test]
        Origins:
          - Id: s3
            DomainName: assets.s3.us-east-1.amazonaws.com
            S3OriginConfig: {}
        DefaultCacheBehavior:
          TargetOriginId: s3
          ViewerProtocolPolicy: allow-all
          CachePolicyId: !Ref AwsCloudfrontCachePolicyAssets
        CustomErrorResponses:
          - ErrorCode: 404
            ResponseCode: 200
            ResponsePagePath: /index.html
```

Feed `template.yaml` straight to `localfront serve --template template.yaml …`.

## Relationship to localfront

This is the *"Terraform plan → CloudFormation companion converter (separate repository)"* on [localfront's roadmap](https://github.com/mackee/localfront#roadmap-post-poc). localfront is the data plane — a local CloudFront emulator; this project is the on-ramp that lets Terraform-defined distributions drive it. The two are versioned independently.

## Development

```console
$ go build ./...
$ go test ./...            # golden tests: examples/<name>/plan.json -> template.yaml
```

The golden tests run entirely from the committed `plan.json` fixtures, so they
need neither terraform nor network. They compare the converted output to each
golden template with a CloudFront-aware canonical form: mappings are
order-independent, set-typed lists (`Origins`, `AllowedMethods`,
`CustomErrorResponses`, …) are compared as multisets, `CacheBehaviors` keeps its
precedence order, and `!Ref` / `!GetAtt` are matched in both YAML short form and
JSON long form.

When you change an example's Terraform, regenerate its fixture (requires
terraform + network for the AWS provider):

```console
$ examples/regenerate.sh
$ go test ./...
```

## Roadmap

- Emit the remaining CloudFront resource types (cache / origin-request / response-headers policies, key groups, public keys, origin access control).
- Accept current-state JSON (`terraform show -json` with no plan), inlining already-known IDs.
- `--validate`: warn about resources or properties localfront will skip or ignore at serve time.
- Optionally invoke `terraform show -json` on a `.tfplan` directly, removing the manual step.
- Traverse child modules (only root-module resources are converted today).
