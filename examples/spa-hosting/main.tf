terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

# Credentials are dummies: a create-only plan never calls AWS, and the
# checks below keep `terraform plan` fully offline.
provider "aws" {
  region                      = "us-east-1"
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true
}

# SPA hosting (e.g. a React/Vite build) served from an S3 origin.
# Converts to examples/spa-hosting/template.yaml.
resource "aws_cloudfront_distribution" "spa_distribution" {
  enabled             = true
  default_root_object = "index.html"
  aliases             = ["spa.example.test"]

  origin {
    origin_id   = "s3"
    domain_name = "spa-assets.s3.us-east-1.amazonaws.com"
    s3_origin_config {
      origin_access_identity = ""
    }
  }

  default_cache_behavior {
    target_origin_id       = "s3"
    viewer_protocol_policy = "allow-all"
    compress               = true
    cache_policy_id        = "658327ea-f89d-4fab-a63d-7e88639e58f6" # Managed-CachingOptimized
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
  }

  # Classic SPA fallback — deep links resolve to the app shell.
  custom_error_response {
    error_code         = 403
    response_code      = 200
    response_page_path = "/index.html"
  }
  custom_error_response {
    error_code         = 404
    response_code      = 200
    response_page_path = "/index.html"
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
