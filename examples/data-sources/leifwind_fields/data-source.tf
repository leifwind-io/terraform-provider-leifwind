# List every field of an entity.
data "leifwind_fields" "all" {
  project_id = "00000000-0000-0000-0000-000000000000"
  entity_id  = "00000000-0000-0000-0000-000000000001"
}

# List fields whose name contains "tit".
data "leifwind_fields" "filtered" {
  project_id = "00000000-0000-0000-0000-000000000000"
  entity_id  = "00000000-0000-0000-0000-000000000001"
  pattern    = "tit"
}
