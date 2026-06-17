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

# Private S3 content fronted by Origin Access Control, with signed-URL access
# gated by a key group. Exercises AWS::CloudFront::PublicKey, KeyGroup, and
# OriginAccessControl, and the cross-resource references to each.
# Converts to examples/signed-private-content/template.yaml.
resource "aws_cloudfront_public_key" "signing_key" {
  name        = "url-signing-key"
  comment     = "Public key for verifying signed URLs"
  encoded_key = file("${path.module}/public_key.pem")
}

resource "aws_cloudfront_key_group" "signers" {
  name    = "url-signers"
  comment = "Key group authorized to sign URLs"
  items   = [aws_cloudfront_public_key.signing_key.id]
}

resource "aws_cloudfront_origin_access_control" "assets" {
  name                              = "private-assets-oac"
  description                       = "SigV4 signing for the private S3 origin"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_cloudfront_distribution" "private" {
  enabled = true
  aliases = ["private.example.test"]

  origin {
    origin_id                = "s3"
    domain_name              = "private-assets.s3.us-east-1.amazonaws.com"
    origin_access_control_id = aws_cloudfront_origin_access_control.assets.id
    s3_origin_config {
      origin_access_identity = ""
    }
  }

  default_cache_behavior {
    target_origin_id       = "s3"
    viewer_protocol_policy = "https-only"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    # Managed CachingOptimized.
    cache_policy_id    = "658327ea-f89d-4fab-a63d-7e88639e58f6"
    trusted_key_groups = [aws_cloudfront_key_group.signers.id]
  }

  restrictions {
    geo_restriction {
      restriction_type = "whitelist"
      locations        = ["US", "JP"]
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
