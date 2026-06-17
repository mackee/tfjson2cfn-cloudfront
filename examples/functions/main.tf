terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
}

# CloudFront Functions: URL normalization, a legacy-path redirect, and a
# KeyValueStore feature flag. Converts to examples/functions/template.yaml.
#
# This example exercises cross-resource references: the function's KVS
# association and the distribution's function association are both
# "known after apply", so the converter restores them as Fn::GetAtt.
resource "aws_cloudfront_key_value_store" "feature_flags" {
  name = "feature-flags"
}

resource "aws_cloudfront_function" "router" {
  name    = "router"
  runtime = "cloudfront-js-2.0"
  comment = "URL normalization, redirect, and a feature flag"
  publish = true
  code    = file("${path.module}/router.js")

  key_value_store_associations = [aws_cloudfront_key_value_store.feature_flags.arn]
}

resource "aws_cloudfront_distribution" "distribution" {
  enabled = true
  aliases = ["app.example.test"]

  origin {
    origin_id   = "app"
    domain_name = "127.0.0.1"
    custom_origin_config {
      http_port              = 3000
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id         = "app"
    viewer_protocol_policy   = "allow-all"
    allowed_methods          = ["GET", "HEAD"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad" # CachingDisabled
    origin_request_policy_id = "216adef6-5c7f-47e4-b989-5492eafa07d3" # AllViewer

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.router.arn
    }
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
