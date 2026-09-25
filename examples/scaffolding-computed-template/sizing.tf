# atmos:template
locals {
  sizing = <% .Config.sizing | toJson %>
}
