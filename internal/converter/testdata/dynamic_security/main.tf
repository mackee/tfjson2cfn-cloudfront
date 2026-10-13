terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
      version = "6.50.0"
    }
  }
}
provider "aws" {
  region = "us-east-1"
  access_key = "test"
  secret_key = "test"
  skip_credentials_validation = true
  skip_requesting_account_id = true
  skip_metadata_api_check = true
}
locals {
  sites = toset(["alpha", "beta"])
  signed = { trusted_key_groups = ["external-signing-id"] }
  default_behavior = local.signed
  paths = ["/api/*", "/session/start", "/session/callback", "/session/bootstrap", "/session/refresh"]
}
resource "aws_cloudfront_response_headers_policy" "headers" {
  for_each = local.sites
  name = "headers-${each.key}"
  security_headers_config {
    content_security_policy {
      content_security_policy = "frame-ancestors https://${each.key}.example.test"
      override = true
    }
  }
}
resource "aws_cloudfront_distribution" "sites" {
  for_each = local.sites
  enabled = true
  origin {
    origin_id = "storage"
    domain_name = "storage.example.test"
    custom_origin_config {
      http_port = 80
      https_port = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols = ["TLSv1.2"]
    }
  }
  default_cache_behavior {
    target_origin_id = "storage"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods = ["GET", "HEAD"]
    cached_methods = ["GET", "HEAD"]
    cache_policy_id = "external-cache-id"
    trusted_key_groups = local.default_behavior.trusted_key_groups
    response_headers_policy_id = aws_cloudfront_response_headers_policy.headers[each.key].id
  }
  dynamic "ordered_cache_behavior" {
    for_each = local.paths
    content {
      path_pattern = ordered_cache_behavior.value
      target_origin_id = "storage"
      viewer_protocol_policy = "redirect-to-https"
      allowed_methods = ["GET", "HEAD"]
      cached_methods = ["GET", "HEAD"]
      cache_policy_id = "external-cache-id"
      trusted_key_groups = ordered_cache_behavior.value == "/api/*" ? local.signed.trusted_key_groups : []
      response_headers_policy_id = aws_cloudfront_response_headers_policy.headers[each.key].id
    }
  }
  restrictions {
    geo_restriction { restriction_type = "none" }
  }
  viewer_certificate { cloudfront_default_certificate = true }
}
