# tfjson2cfn-cloudfront

**Convert a Terraform plan into a CloudFront-only CloudFormation template — the Terraform on-ramp for [localfront](https://github.com/mackee/localfront).**

`tfjson2cfn-cloudfront` reads the JSON that `terraform show -json` emits for a plan and writes a CloudFormation template containing just the `AWS::CloudFront::*` resources. Terraform resolves known values in the plan, while apply-time values can remain unknown. The converter never interprets HCL itself: it reshapes known values into CloudFormation's schema and restores cross-resource links as `Ref` / `Fn::GetAtt` where sufficient reference information is available. The resulting template is what you feed to localfront, so Terraform users get the same instant local CloudFront emulation that CloudFormation and CDK users already have.

> **Status: Proof of Concept.** This README defines the intended scope and CLI. Behavior, flags, and output may change without notice.

## Why

localfront is driven entirely by CloudFormation templates — it has no management API and reads no Terraform. CDK users `cdk synth`; CloudFormation users point it at their template directly. Terraform users had no on-ramp.

A general-purpose Terraform→CloudFormation converter is intractable: it would have to re-implement HCL evaluation, every provider schema, and the entire resource catalog. Two constraints make *this* one tractable:

1. **Consume the plan, not the HCL.** `terraform show -json <planfile>` contains known values and metadata for apply-time unknowns. Variables, `for_each`, `locals`, data sources, and interpolation are evaluated by Terraform itself; not every resulting attribute is known before apply.
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
# or download a pre-built binary for your OS/arch from the GitHub Releases page

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
| `--references` | none | JSON file with explicit references for unknown distribution security fields |
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
| `aws_cloudfront_cache_policy` | `AWS::CloudFront::CachePolicy` | ✅ implemented |
| `aws_cloudfront_origin_request_policy` | `AWS::CloudFront::OriginRequestPolicy` | ✅ implemented |
| `aws_cloudfront_response_headers_policy` | `AWS::CloudFront::ResponseHeadersPolicy` | ✅ implemented |
| `aws_cloudfront_public_key` | `AWS::CloudFront::PublicKey` | ✅ implemented |
| `aws_cloudfront_key_group` | `AWS::CloudFront::KeyGroup` | ✅ implemented |
| `aws_cloudfront_origin_access_control` | `AWS::CloudFront::OriginAccessControl` | ✅ implemented |

All nine types cover the `examples/`, which are golden-tested against the templates [localfront](https://github.com/mackee/localfront) ships. A distribution's references *to* the policy and key resources (`cache_policy_id`, `trusted_key_groups`, `origin_access_control_id`, …) resolve to `Ref` / `Fn::GetAtt` against the converted logical ID. A CloudFront type the converter does not handle (`aws_cloudfront_monitoring_subscription`, `aws_cloudfront_origin_access_identity`, …) is skipped with a warning rather than translated; any non-CloudFront resource (`aws_s3_bucket`, `aws_iam_*`, …) is ignored silently.

### Schema translation

Terraform and CloudFormation describe the same CloudFront concepts with different shapes; the converter knows both:

- Repeated blocks become arrays: `origin { … }` → `Origins: [ … ]`, `ordered_cache_behavior { … }` → `CacheBehaviors: [ … ]`, `custom_error_response { … }` → `CustomErrorResponses: [ … ]`.
- The flat `aws_cloudfront_distribution` body is nested under `DistributionConfig` (and likewise `CachePolicyConfig`, `OriginRequestPolicyConfig`, … for the policy resources).
- snake_case → PascalCase, with CloudFront's naming quirks (`is_ipv6_enabled` → `IPV6Enabled`, `origin_ssl_protocols` → `OriginSSLProtocols`, `viewer_certificate` → `ViewerCertificate`, …).
- Legacy `forwarded_values` is carried over as-is (localfront accepts it).
- Every value the plan resolves is emitted, including schema defaults Terraform fills in (a custom origin's `https_port = 443`, `origin_ssl_protocols`, `origin_read_timeout`; a behavior's `cached_methods`). The output is faithful to the resolved plan rather than to the minimal HCL you wrote, which keeps a committed template's diff meaningful. Truly absent values are omitted (never emitted as `null`).

### Reference resolution

Inside a plan, an attribute that points at another resource — e.g. `cache_policy_id = aws_cloudfront_cache_policy.assets.id` — is "known after apply" and so is absent from the resolved values. The converter detects the dependency from the plan's `configuration` section and restores it as a CloudFormation intrinsic against the converted logical ID:

| Terraform reference | Emitted |
| --- | --- |
| `cache_policy_id = aws_cloudfront_cache_policy.assets.id` | `CachePolicyId: !Ref AwsCloudfrontCachePolicyAssets` |
| `function_arn = aws_cloudfront_function.router.arn` | `FunctionARN: !GetAtt AwsCloudfrontFunctionRouter.FunctionARN` |
| `trusted_key_groups = [aws_cloudfront_key_group.signers.id]` | `TrustedKeyGroups: [ !Ref AwsCloudfrontKeyGroupSigners ]` |
| `key_value_store_arn = aws_cloudfront_key_value_store.flags.arn` | `KeyValueStoreARN: !GetAtt AwsCloudfrontKeyValueStoreFlags.Arn` |

Managed policy IDs (`Managed-CachingOptimized`, …) and other literal values are passed through unchanged — localfront resolves those itself.

Known policy, key group, and origin access control IDs are restored to references only when exactly one managed resource of the expected type has the same nonempty ID. External or ambiguous IDs remain literal. Known trusted key group lists, including empty lists, take precedence over configuration dependencies.

### Unknown security references

Terraform's [JSON format](https://developer.hashicorp.com/terraform/internals/json-format#expression-representation) omits expressions inside `dynamic` blocks. Its `references` arrays describe dependencies rather than complete expressions: a collection reference and `each.key` do not prove which resource instance was selected. The converter does not guess instance relationships or copy a default behavior's policy to ordered behaviors.

Unresolved unknown cache policy, origin request policy, response headers policy, trusted key group, and origin access control references cause conversion to fail before writing a template. An omitted optional `trusted_key_groups` field in a represented static configuration block is treated as unconfigured even if the provider marks its default as unknown. Absent dynamic block expressions do not establish that exception.

To resolve missing information, supply an explicit JSON file:

```json
{
  "aws_cloudfront_distribution.sites[\"alpha\"]": {
    "default_cache_behavior[0].response_headers_policy_id": [
      "aws_cloudfront_response_headers_policy.headers[\"alpha\"]"
    ],
    "ordered_cache_behavior[0].response_headers_policy_id": [
      "aws_cloudfront_response_headers_policy.headers[\"alpha\"]"
    ]
  }
}
```

```console
$ tfjson2cfn-cloudfront -i plan.json --references references.json -o template.yaml
```

Keys are exact root distribution addresses, then Terraform attribute paths. Block indexes refer to the order in `planned_values` (`default_cache_behavior[0]`, `ordered_cache_behavior[N]`, or `origin[N]`). Each scalar field requires one exact managed resource address of the appropriate type; `trusted_key_groups` accepts a complete nonempty list of key group addresses. Hints only apply to unknown fields; missing resources, wrong types, duplicate targets, unknown distributions, and unused paths are errors. Known empty signer lists cannot be overridden. The caller owns the correctness of these explicit relationships and must regenerate or review hints when block order changes.

Logical IDs are derived deterministically from the Terraform resource's full address — the resource type and local name PascalCased on underscores (`aws_cloudfront_distribution.assets` → `AwsCloudfrontDistributionAssets`, `aws_cloudfront_cache_policy.long_cache` → `AwsCloudfrontCachePolicyLongCache`), so the output is stable across runs. Alphanumeric string instance keys use a `Key` marker followed by their original case; other string keys use a `Hash` suffix. Numeric indexes use `Index`, keeping string and numeric instance keys distinct. Because the type is part of the ID, resources of different kinds that share a local name (e.g. an origin access control, a key group and a public key all named `tools`) each get a distinct logical ID instead of colliding.

## Not supported / out of scope

- **Non-CloudFront resources** — only `aws_cloudfront_*` is translated, by design.
- **HCL evaluation** — the converter reads resolved plan JSON; it never parses `.tf` files. Unknown distribution security references fail conversion unless they can be restored or explicitly supplied through `--references`. Other unknown properties may still be omitted; conversion does not evaluate HCL or promise completeness for every unknown field.
- **Round-tripping back to AWS** — output is one-way, intended for localfront, not for `aws cloudformation deploy`.
- **Property-level fidelity for what localfront ignores** — properties are still translated when present, but see localfront's *Accepted but ignored* / *Not implemented* lists for what actually takes effect at serve time.

## Example

```hcl
resource "aws_cloudfront_cache_policy" "assets_cache" {
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
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = aws_cloudfront_cache_policy.assets_cache.id
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
  AwsCloudfrontCachePolicyAssetsCache:
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
          AllowedMethods: [GET, HEAD]
          CachedMethods: [GET, HEAD]
          CachePolicyId: !Ref AwsCloudfrontCachePolicyAssetsCache
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
$ golangci-lint run ./...  # installed via aqua (aqua i)
```

CI (`.github/workflows/ci.yml`) runs `go vet` / gofmt / `go build` / `go test`
and golangci-lint on every push and pull request. Releases are cut by
`.github/workflows/release.yml`: pushing a `vX.Y.Z` tag runs
[GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`), which builds the
cross-platform binaries and publishes a GitHub Release. Dry-run locally with
`goreleaser release --snapshot --clean`. GitHub Actions are pinned to commit
SHAs with [pinact](https://github.com/suzuki-shunsuke/pinact) (`pinact run`);
pinact, golangci-lint, and goreleaser all come from `aqua.yaml`.

`TestExamples` discovers every `examples/<name>/` with a `plan.json` and a
`template.yaml`, converts the plan, and checks the result is CloudFront-
equivalent to the golden template. It runs entirely from the committed
`plan.json` fixtures, so it needs neither terraform nor network. The comparison
uses a CloudFront-aware canonical form: mappings are order-independent, set-typed
lists (`Origins`, `AllowedMethods`, `CustomErrorResponses`, …) are compared as
multisets, `CacheBehaviors` keeps its precedence order, and `!Ref` / `!GetAtt`
match in both YAML short form and JSON long form. Add a case by dropping a new
`examples/<name>/` directory — no test code changes.

The fixtures are regenerated from their Terraform by the same suite under
`-update` (requires terraform — see `aqua.yaml` — and network for the AWS
provider). It runs `terraform init/plan/show -json` per example and rewrites
`plan.json` (dropping the volatile timestamp, sorted and indented):

```console
$ go test ./internal/converter -update
$ go test ./...                          # verify, then commit the fixtures
```

## Roadmap

- Accept current-state JSON (`terraform show -json` with no plan), inlining already-known IDs.
- `--validate`: warn about resources or properties localfront will skip or ignore at serve time.
- Optionally invoke `terraform show -json` on a `.tfplan` directly, removing the manual step.
- Traverse child modules (only root-module resources are converted today).
