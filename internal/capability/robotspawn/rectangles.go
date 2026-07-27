package robotspawn

import (
	"math"

	"robot/internal/shared"
)

type RangeRandom interface {
	RandBetween(min, max int) int
}

func MapRectangles(mp shared.MapCatalogItem) []shared.MapRectangle {
	if len(mp.Rectangles) > 0 {
		out := make([]shared.MapRectangle, 0, len(mp.Rectangles))
		for _, rectangle := range mp.Rectangles {
			if ValidRectangle(rectangle) {
				out = append(out, rectangle)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	fallback := shared.MapRectangle{XMin: mp.XMin, XMax: mp.XMax, YMin: mp.YMin, YMax: mp.YMax}
	if !ValidRectangle(fallback) {
		return nil
	}
	return []shared.MapRectangle{fallback}
}

func ValidRectangle(rectangle shared.MapRectangle) bool {
	return rectangle.XMax >= rectangle.XMin && rectangle.YMax >= rectangle.YMin
}

func RectangleContains(rectangle shared.MapRectangle, x, y int) bool {
	return x >= rectangle.XMin && x <= rectangle.XMax && y >= rectangle.YMin && y <= rectangle.YMax
}

func RectangleArea(rectangle shared.MapRectangle) int {
	width := rectangle.XMax - rectangle.XMin + 1
	height := rectangle.YMax - rectangle.YMin + 1
	if width <= 0 || height <= 0 {
		return 0
	}
	return width * height
}

func SmoothedRectangleWeight(rectangle shared.MapRectangle) int {
	area := RectangleArea(rectangle)
	if area <= 0 {
		return 1
	}
	weight := int(math.Sqrt(float64(area)))
	if weight < 1 {
		return 1
	}
	return weight
}

func IntersectRectangles(rectangles []shared.MapRectangle, bounds shared.MapRectangle) []shared.MapRectangle {
	if !ValidRectangle(bounds) {
		return nil
	}
	out := make([]shared.MapRectangle, 0, len(rectangles))
	for _, rectangle := range rectangles {
		intersection := shared.MapRectangle{
			XMin: maxInt(rectangle.XMin, bounds.XMin),
			XMax: minInt(rectangle.XMax, bounds.XMax),
			YMin: maxInt(rectangle.YMin, bounds.YMin),
			YMax: minInt(rectangle.YMax, bounds.YMax),
		}
		if ValidRectangle(intersection) {
			out = append(out, intersection)
		}
	}
	return out
}

func RandomPoint(env RangeRandom, rectangles []shared.MapRectangle) (x, y int, ok bool) {
	if env == nil || len(rectangles) == 0 {
		return 0, 0, false
	}
	totalWeight := 0
	for _, rectangle := range rectangles {
		totalWeight += SmoothedRectangleWeight(rectangle)
	}
	choice := env.RandBetween(1, totalWeight)
	if choice < 1 || choice > totalWeight {
		choice = 1
	}
	selected := rectangles[0]
	for _, rectangle := range rectangles {
		choice -= SmoothedRectangleWeight(rectangle)
		if choice <= 0 {
			selected = rectangle
			break
		}
	}
	return env.RandBetween(selected.XMin, selected.XMax), env.RandBetween(selected.YMin, selected.YMax), true
}

func RandomPointInMap(env RangeRandom, mp shared.MapCatalogItem) (x, y int, ok bool) {
	return RandomPoint(env, MapRectangles(mp))
}

func PointInMap(mp shared.MapCatalogItem, x, y int) bool {
	for _, rectangle := range MapRectangles(mp) {
		if RectangleContains(rectangle, x, y) {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
