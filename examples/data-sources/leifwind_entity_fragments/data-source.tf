# List the fragment names derived from an entity's FRAGMENT-connection
# fields.
data "leifwind_entity_fragments" "book_fragments" {
  project_id  = "00000000-0000-0000-0000-000000000000"
  entity_name = "book"
}
