# Recursively sort arrays that don't have semantic ordering
# This normalizes JSON for comparison purposes

def normalize:
  if type == "array" then
    map(normalize) | sort_by(
      if type == "object" then
        # Sort objects by their key fields for deterministic ordering
        (.address // .key // .contract_address // .transaction_hash // "")
      else
        .
      end
    )
  elif type == "object" then
    # Recursively normalize all values
    with_entries(.value |= normalize)
  else
    .
  end;

normalize
