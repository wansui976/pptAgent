package server

import (
	"encoding/xml"
	"fmt"
	"html"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type svgNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []svgNode  `xml:",any"`
	Text     string     `xml:",chardata"`
}

type svgContext struct {
	scaleX float64
	scaleY float64
}

func convertSVGFileToPPTistElements(svgPath string) ([]map[string]any, map[string]any, error) {
	content, err := os.ReadFile(svgPath)
	if err != nil {
		return nil, nil, err
	}
	var root svgNode
	if err := xml.Unmarshal(content, &root); err != nil {
		return nil, nil, err
	}
	viewBox := parseViewBox(attrValue(root.Attrs, "viewBox"))
	width := parseFloatDefault(attrValue(root.Attrs, "width"), viewBox[2])
	height := parseFloatDefault(attrValue(root.Attrs, "height"), viewBox[3])
	if width <= 0 {
		width = viewBox[2]
	}
	if height <= 0 {
		height = viewBox[3]
	}
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	ctx := svgContext{scaleX: 1000 / width, scaleY: 562.5 / height}
	background := map[string]any{"type": "solid", "color": "#ffffff"}
	elements := make([]map[string]any, 0)
	walkSVGNodes(root.Children, ctx, &elements, &background, inheritedStyle{})
	return elements, background, nil
}

type inheritedStyle map[string]string

func walkSVGNodes(nodes []svgNode, ctx svgContext, elements *[]map[string]any, background *map[string]any, inherited inheritedStyle) {
	for _, node := range nodes {
		name := localName(node.XMLName.Local)
		if name == "defs" || name == "style" || name == "title" || name == "desc" {
			continue
		}
		style := mergeStyle(inherited, node.Attrs)
		switch name {
		case "g", "svg":
			walkSVGNodes(node.Children, ctx, elements, background, style)
		case "rect":
			if el := convertRect(node, ctx, style); el != nil {
				if len(*elements) == 0 && isFullSlideShape(el) && shapeFill(el) != "#00000000" {
					*background = map[string]any{"type": "solid", "color": shapeFill(el)}
				} else {
					*elements = append(*elements, el)
				}
			}
		case "circle", "ellipse":
			if el := convertEllipse(node, ctx, style); el != nil {
				*elements = append(*elements, el)
			}
		case "line":
			if el := convertLine(node, ctx, style); el != nil {
				*elements = append(*elements, el)
			}
		case "path":
			if el := convertPath(node, ctx, style); el != nil {
				*elements = append(*elements, el)
			}
		case "text":
			if el := convertText(node, ctx, style); el != nil {
				*elements = append(*elements, el)
			}
		case "image":
			if el := convertImage(node, ctx, style); el != nil {
				*elements = append(*elements, el)
			}
		}
	}
}

func localName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func attrValue(attrs []xml.Attr, keys ...string) string {
	for _, key := range keys {
		for _, attr := range attrs {
			if strings.EqualFold(attr.Name.Local, key) {
				return strings.TrimSpace(attr.Value)
			}
		}
	}
	return ""
}

func mergeStyle(parent inheritedStyle, attrs []xml.Attr) inheritedStyle {
	merged := inheritedStyle{}
	for key, value := range parent {
		merged[key] = value
	}
	for _, attr := range attrs {
		key := strings.ToLower(attr.Name.Local)
		if key == "style" {
			for _, part := range strings.Split(attr.Value, ";") {
				pair := strings.SplitN(part, ":", 2)
				if len(pair) == 2 {
					merged[strings.ToLower(strings.TrimSpace(pair[0]))] = strings.TrimSpace(pair[1])
				}
			}
			continue
		}
		merged[key] = strings.TrimSpace(attr.Value)
	}
	return merged
}

func styleValue(style inheritedStyle, key string, fallback string) string {
	if value := strings.TrimSpace(style[strings.ToLower(key)]); value != "" {
		return value
	}
	return fallback
}

func parseViewBox(raw string) [4]float64 {
	parts := splitNumberList(raw)
	if len(parts) >= 4 {
		return [4]float64{parts[0], parts[1], parts[2], parts[3]}
	}
	return [4]float64{0, 0, 1280, 720}
}

func splitNumberList(raw string) []float64 {
	re := regexp.MustCompile(`[-+]?\d*\.?\d+(?:[eE][-+]?\d+)?`)
	matches := re.FindAllString(raw, -1)
	values := make([]float64, 0, len(matches))
	for _, match := range matches {
		if value, err := strconv.ParseFloat(match, 64); err == nil {
			values = append(values, value)
		}
	}
	return values
}

func parseFloatDefault(raw string, fallback float64) float64 {
	values := splitNumberList(raw)
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

func scaledX(value float64, ctx svgContext) float64 { return round2(value * ctx.scaleX) }
func scaledY(value float64, ctx svgContext) float64 { return round2(value * ctx.scaleY) }
func round2(value float64) float64                  { return math.Round(value*100) / 100 }

func newElementID() string { return strings.ReplaceAll(uuid.NewString(), "-", "")[:10] }

func normalizeColor(raw string, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	lower := strings.ToLower(value)
	if lower == "none" || lower == "transparent" {
		return "#00000000"
	}
	if strings.HasPrefix(lower, "url(") {
		return fallback
	}
	return value
}

func opacity(style inheritedStyle) float64 {
	value := parseFloatDefault(styleValue(style, "opacity", "1"), 1)
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func outline(style inheritedStyle) map[string]any {
	stroke := normalizeColor(styleValue(style, "stroke", "none"), "#00000000")
	if stroke == "#00000000" {
		return nil
	}
	width := parseFloatDefault(styleValue(style, "stroke-width", "1"), 1)
	return map[string]any{"width": width, "color": stroke, "style": "solid"}
}

func shapeElement(left, top, width, height float64, path string, viewBox []float64, fill string, style inheritedStyle) map[string]any {
	if width <= 0 || height <= 0 {
		return nil
	}
	el := map[string]any{
		"type":       "shape",
		"id":         newElementID(),
		"left":       round2(left),
		"top":        round2(top),
		"width":      round2(width),
		"height":     round2(height),
		"viewBox":    viewBox,
		"path":       path,
		"fill":       fill,
		"fixedRatio": false,
		"rotate":     0,
		"opacity":    opacity(style),
	}
	if outline := outline(style); outline != nil {
		el["outline"] = outline
	}
	return el
}

func convertRect(node svgNode, ctx svgContext, style inheritedStyle) map[string]any {
	x := parseFloatDefault(attrValue(node.Attrs, "x"), 0)
	y := parseFloatDefault(attrValue(node.Attrs, "y"), 0)
	w := parseFloatDefault(attrValue(node.Attrs, "width"), 0)
	h := parseFloatDefault(attrValue(node.Attrs, "height"), 0)
	fill := normalizeColor(styleValue(style, "fill", "#000000"), "#000000")
	return shapeElement(scaledX(x, ctx), scaledY(y, ctx), scaledX(w, ctx), scaledY(h, ctx), "M 0 0 L 200 0 L 200 200 L 0 200 Z", []float64{200, 200}, fill, style)
}

func convertEllipse(node svgNode, ctx svgContext, style inheritedStyle) map[string]any {
	name := localName(node.XMLName.Local)
	cx := parseFloatDefault(attrValue(node.Attrs, "cx"), 0)
	cy := parseFloatDefault(attrValue(node.Attrs, "cy"), 0)
	rx := parseFloatDefault(attrValue(node.Attrs, "rx"), 0)
	ry := parseFloatDefault(attrValue(node.Attrs, "ry"), 0)
	if name == "circle" {
		r := parseFloatDefault(attrValue(node.Attrs, "r"), 0)
		rx, ry = r, r
	}
	fill := normalizeColor(styleValue(style, "fill", "#000000"), "#000000")
	path := "M 100 0 A 100 100 0 1 1 100 200 A 100 100 0 1 1 100 0 Z"
	return shapeElement(scaledX(cx-rx, ctx), scaledY(cy-ry, ctx), scaledX(rx*2, ctx), scaledY(ry*2, ctx), path, []float64{200, 200}, fill, style)
}

func convertLine(node svgNode, ctx svgContext, style inheritedStyle) map[string]any {
	x1 := scaledX(parseFloatDefault(attrValue(node.Attrs, "x1"), 0), ctx)
	y1 := scaledY(parseFloatDefault(attrValue(node.Attrs, "y1"), 0), ctx)
	x2 := scaledX(parseFloatDefault(attrValue(node.Attrs, "x2"), 0), ctx)
	y2 := scaledY(parseFloatDefault(attrValue(node.Attrs, "y2"), 0), ctx)
	left := math.Min(x1, x2)
	top := math.Min(y1, y2)
	return map[string]any{
		"type":   "line",
		"id":     newElementID(),
		"left":   round2(left),
		"top":    round2(top),
		"start":  []float64{round2(x1 - left), round2(y1 - top)},
		"end":    []float64{round2(x2 - left), round2(y2 - top)},
		"points": []string{"", ""},
		"color":  normalizeColor(styleValue(style, "stroke", "#000000"), "#000000"),
		"style":  "solid",
		"width":  parseFloatDefault(styleValue(style, "stroke-width", "1"), 1),
	}
}

func convertPath(node svgNode, ctx svgContext, style inheritedStyle) map[string]any {
	d := strings.TrimSpace(attrValue(node.Attrs, "d"))
	if d == "" {
		return nil
	}
	bounds := pathBounds(d)
	if bounds == nil {
		return nil
	}
	minX, minY, maxX, maxY := bounds[0], bounds[1], bounds[2], bounds[3]
	fill := normalizeColor(styleValue(style, "fill", "#000000"), "#000000")
	el := shapeElement(scaledX(minX, ctx), scaledY(minY, ctx), scaledX(maxX-minX, ctx), scaledY(maxY-minY, ctx), d, []float64{maxX - minX, maxY - minY}, fill, style)
	if el != nil {
		el["special"] = true
	}
	return el
}

func pathBounds(d string) []float64 {
	values := splitNumberList(d)
	if len(values) < 2 {
		return nil
	}
	minX, maxX := values[0], values[0]
	minY, maxY := values[1], values[1]
	for i := 0; i+1 < len(values); i += 2 {
		x, y := values[i], values[i+1]
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	if maxX == minX || maxY == minY {
		return nil
	}
	return []float64{minX, minY, maxX, maxY}
}

func convertText(node svgNode, ctx svgContext, style inheritedStyle) map[string]any {
	text := collectText(node)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	x := parseFloatDefault(attrValue(node.Attrs, "x"), 0)
	y := parseFloatDefault(attrValue(node.Attrs, "y"), 0)
	fontSize := parseFloatDefault(styleValue(style, "font-size", "24"), 24)
	fontWeight := styleValue(style, "font-weight", "")
	fontFamily := strings.Trim(styleValue(style, "font-family", ""), "'\"")
	color := normalizeColor(styleValue(style, "fill", "#333333"), "#333333")
	content := html.EscapeString(strings.TrimSpace(text))
	if fontWeight == "bold" || parseFloatDefault(fontWeight, 0) >= 600 {
		content = "<strong>" + content + "</strong>"
	}
	xAlign := strings.ToLower(styleValue(style, "text-anchor", "start"))
	textAlign := "left"
	width := math.Max(float64(len([]rune(text)))*fontSize*0.68, fontSize*2)
	left := x
	if xAlign == "middle" {
		textAlign = "center"
		left = x - width/2
	} else if xAlign == "end" {
		textAlign = "right"
		left = x - width
	}
	height := fontSize * 1.45
	return map[string]any{
		"type":            "text",
		"id":              newElementID(),
		"left":            scaledX(left, ctx),
		"top":             scaledY(y-fontSize, ctx),
		"width":           scaledX(width, ctx),
		"height":          scaledY(height, ctx),
		"content":         fmt.Sprintf(`<p style="text-align: %s;"><span style="font-size: %.0fpx;"><span style="color: %s;">%s</span></span></p>`, textAlign, fontSize, color, content),
		"rotate":          0,
		"defaultFontName": fontFamily,
		"defaultColor":    color,
		"vertical":        false,
	}
}

func collectText(node svgNode) string {
	var builder strings.Builder
	var walk func(svgNode)
	walk = func(n svgNode) {
		if strings.TrimSpace(n.Text) != "" {
			builder.WriteString(n.Text)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}

func convertImage(node svgNode, ctx svgContext, style inheritedStyle) map[string]any {
	href := attrValue(node.Attrs, "href", "xlink:href")
	if href == "" {
		return nil
	}
	x := parseFloatDefault(attrValue(node.Attrs, "x"), 0)
	y := parseFloatDefault(attrValue(node.Attrs, "y"), 0)
	w := parseFloatDefault(attrValue(node.Attrs, "width"), 0)
	h := parseFloatDefault(attrValue(node.Attrs, "height"), 0)
	return map[string]any{
		"type":       "image",
		"id":         newElementID(),
		"src":        href,
		"width":      scaledX(w, ctx),
		"height":     scaledY(h, ctx),
		"left":       scaledX(x, ctx),
		"top":        scaledY(y, ctx),
		"fixedRatio": true,
		"rotate":     0,
		"opacity":    opacity(style),
	}
}

func isFullSlideShape(el map[string]any) bool {
	left, _ := el["left"].(float64)
	top, _ := el["top"].(float64)
	width, _ := el["width"].(float64)
	height, _ := el["height"].(float64)
	return left <= 1 && top <= 1 && width >= 998 && height >= 560
}

func shapeFill(el map[string]any) string {
	if fill, ok := el["fill"].(string); ok {
		return fill
	}
	return "#00000000"
}
