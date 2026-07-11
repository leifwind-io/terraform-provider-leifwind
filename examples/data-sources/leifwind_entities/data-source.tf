# List every entity in a project.
data "leifwind_entities" "all" {
  project_id = "00000000-0000-0000-0000-000000000000"
}

# List entities whose name contains "boo".
data "leifwind_entities" "filtered" {
  project_id = "00000000-0000-0000-0000-000000000000"
  pattern    = "boo"
}
