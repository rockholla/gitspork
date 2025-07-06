# ::gitspork::begin-upstream-owned-block::01
terraform {
  required_version = ">= 1.0.0"
}
# ::gitspork::end-upstream-owned-block

# ::gitspork::begin-upstream-owned-block::02
locals {
  managed_val_one = "one"
  managed_val_two = "two"
}
// ::gitspork::end-upstream-owned-block
