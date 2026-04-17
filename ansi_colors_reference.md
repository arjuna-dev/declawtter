# ANSI Color Codes Reference

## Text Colors (Foreground)
```go
// Basic Colors
ansiBlack   = "\x1b[30m"
ansiRed     = "\x1b[31m"
ansiGreen   = "\x1b[32m"
ansiYellow  = "\x1b[33m"
ansiBlue    = "\x1b[34m"
ansiMagenta = "\x1b[35m"
ansiCyan    = "\x1b[36m"
ansiWhite   = "\x1b[37m"

// Bright Colors
ansiBrightBlack   = "\x1b[90m"
ansiBrightRed     = "\x1b[91m"
ansiBrightGreen   = "\x1b[92m"
ansiBrightYellow  = "\x1b[93m"
ansiBrightBlue    = "\x1b[94m"
ansiBrightMagenta = "\x1b[95m"
ansiBrightCyan    = "\x1b[96m"
ansiBrightWhite   = "\x1b[97m"
```

## Background Colors
```go
// Basic Background Colors
ansiBgBlack   = "\x1b[40m"
ansiBgRed     = "\x1b[41m"
ansiBgGreen   = "\x1b[42m"
ansiBgYellow  = "\x1b[43m"
ansiBgBlue    = "\x1b[44m"
ansiBgMagenta = "\x1b[45m"
ansiBgCyan    = "\x1b[46m"
ansiBgWhite   = "\x1b[47m"

// Bright Background Colors
ansiBgBrightBlack   = "\x1b[100m"
ansiBgBrightRed     = "\x1b[101m"
ansiBgBrightGreen   = "\x1b[102m"
ansiBgBrightYellow  = "\x1b[103m"
ansiBgBrightBlue    = "\x1b[104m"
ansiBgBrightMagenta = "\x1b[105m"
ansiBgBrightCyan    = "\x1b[106m"
ansiBgBrightWhite   = "\x1b[107m"
```

## Text Formatting
```go
// Currently used:
ansiReset = "\x1b[0m"    // Reset all formatting
ansiDim   = "\x1b[2m"    // Dim/faded text

// Additional formatting options:
ansiBold      = "\x1b[1m"    // Bold text
ansiItalic    = "\x1b[3m"    // Italic text
ansiUnderline = "\x1b[4m"    // Underlined text
ansiBlink     = "\x1b[5m"    // Blinking text (not widely supported)
ansiReverse   = "\x1b[7m"    // Reversed (swap fg/bg)
ansiStrike    = "\x1b[9m"    // Strikethrough
```

## Usage Examples

```go
// Add to your constants section:
const (
    ansiReset = "\x1b[0m"
    ansiGreen = "\x1b[32m"
    ansiBlue  = "\x1b[34m"
    ansiDim   = "\x1b[2m"
    ansiRed   = "\x1b[31m"     // For errors
    ansiYellow = "\x1b[33m"    // For warnings
    ansiBold  = "\x1b[1m"      // For emphasis
)

// Usage with existing colorize function:
fmt.Println(colorize("Error:", ansiRed))
fmt.Println(colorize("Warning:", ansiYellow))
fmt.Println(colorize("Success:", ansiGreen))
fmt.Println(colorize("Important:", ansiBold))

// Combining formats (manual):
fmt.Printf("%s%sImportant Error%s\n", ansiBold, ansiRed, ansiReset)
```

## 256-Color and RGB Support

For more advanced colors:

```go
// 256-color mode (0-255)
ansi256Color := func(code int) string {
    return fmt.Sprintf("\x1b[38;5;%dm", code)
}

// RGB color mode
ansiRGB := func(r, g, b int) string {
    return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}
```

## Notes

- ANSI codes only work in terminals that support them
- Your `stdoutIsTerminal()` check is good practice
- Always end with `ansiReset` to avoid bleeding colors
- Some terminals may not support all formatting options