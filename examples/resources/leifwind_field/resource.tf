resource "leifwind_field" "title" {
  project_id      = leifwind_project.library.id
  entity_id       = leifwind_entity.book.id
  name            = "title"
  data_type       = "TEXT"
  connection_type = "KEY"
}

resource "leifwind_field" "body" {
  project_id      = leifwind_project.library.id
  entity_id       = leifwind_entity.book.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"

  # LW-70: force title to be created first (and destroyed last) — the
  # backend 500s if the first field ever created on an entity is a
  # FRAGMENT field, or if a KEY field is deleted while a FRAGMENT
  # sibling still exists.
  depends_on = [leifwind_field.title]
}
