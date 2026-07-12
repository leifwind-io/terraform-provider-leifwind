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

  # A FRAGMENT field needs a sibling KEY field on the entity. Referencing the
  # KEY field's id here makes Terraform create it first and destroy it last —
  # no manual depends_on. List all of the entity's KEY fields.
  key_field_ids = [leifwind_field.title.id]
}
