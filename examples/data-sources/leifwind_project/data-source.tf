# Look up by id.
data "leifwind_project" "by_id" {
  id = "00000000-0000-0000-0000-000000000000"
}

# Look up by exact name.
data "leifwind_project" "by_name" {
  name = "library"
}
