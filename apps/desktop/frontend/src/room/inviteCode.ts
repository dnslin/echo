export type NormalizedInviteCode = {
  value: string
  isComplete: boolean
  isValid: boolean
}

export function normalizeInviteCode(input: string): NormalizedInviteCode {
  let value = ''
  let hasInvalidCharacter = false

  for (const character of input) {
    if (character === '-' || isInviteWhitespace(character)) continue

    const upper = character >= 'a' && character <= 'z' ? character.toUpperCase() : character
    if (!isInviteCharacter(upper)) {
      hasInvalidCharacter = true
      continue
    }
    value += upper
  }

  const isValid = !hasInvalidCharacter && value.length <= 6
  return {
    value,
    isComplete: isValid && value.length === 6,
    isValid,
  }
}

function isInviteCharacter(character: string): boolean {
  return (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')
}

function isInviteWhitespace(character: string): boolean {
  const codePoint = character.codePointAt(0)
  if (codePoint === undefined) return false
  return (codePoint >= 0x0009 && codePoint <= 0x000d)
    || codePoint === 0x0020
    || codePoint === 0x0085
    || codePoint === 0x00a0
    || codePoint === 0x1680
    || (codePoint >= 0x2000 && codePoint <= 0x200a)
    || codePoint === 0x2028
    || codePoint === 0x2029
    || codePoint === 0x202f
    || codePoint === 0x205f
    || codePoint === 0x3000
}
