# List every project.
data "leifwind_projects" "all" {}

# List projects whose name contains "lib".
data "leifwind_projects" "filtered" {
  pattern = "lib"
}
