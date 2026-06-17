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

# Custom cache, origin-request, and response-headers policies attached to a
# distribution. Exercises the three AWS::CloudFront::*Policy resource types and
# the distribution references that resolve to !Ref.
# Converts to examples/custom-policies/template.yaml.
resource "aws_cloudfront_cache_policy" "long_cache" {
  name        = "long-cache"
  comment     = "Cache for a day, keyed on a couple of query strings"
  default_ttl = 86400
  max_ttl     = 31536000
  min_ttl     = 1

  parameters_in_cache_key_and_forwarded_to_origin {
    enable_accept_encoding_brotli = true
    enable_accept_encoding_gzip   = true

    cookies_config {
      cookie_behavior = "none"
    }
    headers_config {
      header_behavior = "whitelist"
      headers {
        items = ["Origin"]
      }
    }
    query_strings_config {
      query_string_behavior = "whitelist"
      query_strings {
        items = ["version", "lang"]
      }
    }
  }
}

resource "aws_cloudfront_origin_request_policy" "forward_all" {
  name    = "forward-all"
  comment = "Forward all cookies and a header allowlist to the origin"

  cookies_config {
    cookie_behavior = "all"
  }
  headers_config {
    header_behavior = "whitelist"
    headers {
      items = ["CloudFront-Viewer-Country", "User-Agent"]
    }
  }
  query_strings_config {
    query_string_behavior = "all"
  }
}

resource "aws_cloudfront_response_headers_policy" "security" {
  name    = "security-headers"
  comment = "CORS plus a few security headers"

  cors_config {
    access_control_allow_credentials = false
    access_control_allow_headers {
      items = ["*"]
    }
    access_control_allow_methods {
      items = ["GET", "HEAD", "OPTIONS"]
    }
    access_control_allow_origins {
      items = ["https://app.example.test"]
    }
    access_control_max_age_sec = 600
    origin_override            = true
  }

  custom_headers_config {
    items {
      header   = "X-Powered-By"
      value    = "localfront"
      override = true
    }
  }

  security_headers_config {
    content_type_options {
      override = true
    }
    frame_options {
      frame_option = "DENY"
      override     = true
    }
    strict_transport_security {
      access_control_max_age_sec = 63072000
      include_subdomains         = true
      preload                    = true
      override                   = true
    }
  }

  server_timing_headers_config {
    enabled       = true
    sampling_rate = 10
  }
}

resource "aws_cloudfront_distribution" "site" {
  enabled = true
  aliases = ["app.example.test"]

  origin {
    origin_id   = "assets"
    domain_name = "app-assets.s3.us-east-1.amazonaws.com"
    s3_origin_config {
      origin_access_identity = ""
    }
  }

  default_cache_behavior {
    target_origin_id           = "assets"
    viewer_protocol_policy     = "redirect-to-https"
    allowed_methods            = ["GET", "HEAD"]
    cached_methods             = ["GET", "HEAD"]
    cache_policy_id            = aws_cloudfront_cache_policy.long_cache.id
    origin_request_policy_id   = aws_cloudfront_origin_request_policy.forward_all.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.security.id
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
