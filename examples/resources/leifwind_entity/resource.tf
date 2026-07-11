resource "leifwind_entity" "book" {
  project_id = leifwind_project.library.id
  name       = "book"
}
