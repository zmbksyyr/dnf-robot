package service

var referenceTownMaps = []mapCatalogItem{
	{Village: 1, Area: 0, Level: 1, XMin: 26, XMax: 1497, YMin: 196, YMax: 337, Use: true},
	{Village: 1, Area: 2, Level: 1, XMin: 26, XMax: 832, YMin: 192, YMax: 319, Use: true},
	{Village: 2, Area: 0, Level: 3, XMin: 26, XMax: 3451, YMin: 226, YMax: 445, Use: true},
	{Village: 2, Area: 1, Level: 3, XMin: 116, XMax: 3638, YMin: 223, YMax: 330, Use: true},
	{Village: 2, Area: 2, Level: 3, XMin: 106, XMax: 2283, YMin: 221, YMax: 336, Use: true},
	{Village: 2, Area: 3, Level: 3, XMin: 89, XMax: 962, YMin: 221, YMax: 336, Use: true},
	{Village: 2, Area: 4, Level: 3, XMin: 132, XMax: 1200, YMin: 172, YMax: 322, Use: true},
	{Village: 2, Area: 6, Level: 3, XMin: 130, XMax: 715, YMin: 144, YMax: 301, Use: true},
	{Village: 2, Area: 7, Level: 3, XMin: 74, XMax: 799, YMin: 217, YMax: 340, Use: true},
	{Village: 2, Area: 8, Level: 3, XMin: 74, XMax: 799, YMin: 217, YMax: 340, Use: true},
	{Village: 2, Area: 9, Level: 3, XMin: 107, XMax: 748, YMin: 210, YMax: 348, Use: true},
	{Village: 3, Area: 0, Level: 13, XMin: 107, XMax: 2783, YMin: 226, YMax: 452, Use: true},
	{Village: 3, Area: 1, Level: 13, XMin: 60, XMax: 1691, YMin: 222, YMax: 329, Use: true},
	{Village: 3, Area: 3, Level: 13, XMin: 60, XMax: 761, YMin: 114, YMax: 301, Use: true},
	{Village: 3, Area: 4, Level: 13, XMin: 60, XMax: 741, YMin: 234, YMax: 335, Use: true},
	{Village: 3, Area: 5, Level: 13, XMin: 60, XMax: 760, YMin: 114, YMax: 301, Use: true},
	{Village: 3, Area: 6, Level: 13, XMin: 60, XMax: 786, YMin: 239, YMax: 350, Use: true},
	{Village: 3, Area: 7, Level: 13, XMin: 70, XMax: 815, YMin: 232, YMax: 353, Use: true},
	{Village: 3, Area: 8, Level: 13, XMin: 70, XMax: 759, YMin: 209, YMax: 337, Use: true},
	{Village: 3, Area: 9, Level: 13, XMin: 105, XMax: 768, YMin: 221, YMax: 344, Use: true},
	{Village: 3, Area: 10, Level: 13, XMin: 95, XMax: 787, YMin: 207, YMax: 345, Use: true},
	{Village: 4, Area: 0, Level: 37, XMin: 72, XMax: 2332, YMin: 184, YMax: 441, Use: true},
	{Village: 4, Area: 2, Level: 37, XMin: 72, XMax: 899, YMin: 173, YMax: 322, Use: true},
	{Village: 4, Area: 3, Level: 37, XMin: 17, XMax: 912, YMin: 183, YMax: 319, Use: true},
	{Village: 4, Area: 4, Level: 37, XMin: 207, XMax: 1011, YMin: 171, YMax: 320, Use: true},
	{Village: 4, Area: 5, Level: 37, XMin: 106, XMax: 811, YMin: 193, YMax: 320, Use: true},
	{Village: 5, Area: 0, Level: 42, XMin: 112, XMax: 2111, YMin: 400, YMax: 900, Use: true},
	{Village: 5, Area: 2, Level: 42, XMin: 80, XMax: 799, YMin: 353, YMax: 582, Use: true},
	{Village: 5, Area: 3, Level: 42, XMin: 95, XMax: 786, YMin: 353, YMax: 582, Use: true},
	{Village: 6, Area: 0, Level: 55, XMin: 122, XMax: 2560, YMin: 211, YMax: 343, Use: true},
	{Village: 6, Area: 1, Level: 55, XMin: 40, XMax: 1839, YMin: 210, YMax: 330, Use: true},
	{Village: 6, Area: 2, Level: 55, XMin: 195, XMax: 831, YMin: 139, YMax: 269, Use: true},
	{Village: 6, Area: 3, Level: 55, XMin: 195, XMax: 831, YMin: 139, YMax: 269, Use: true},
	{Village: 6, Area: 5, Level: 55, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 0, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 1, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 2, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 3, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 4, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 7, Area: 5, Level: 50, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 0, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 1, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 2, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 8, Area: 3, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 9, Area: 0, Level: 65, XMin: 91, XMax: 1458, YMin: 205, YMax: 333, Use: true},
	{Village: 9, Area: 1, Level: 65, XMin: 20, XMax: 772, YMin: 143, YMax: 315, Use: true},
	{Village: 9, Area: 3, Level: 65, XMin: 38, XMax: 813, YMin: 205, YMax: 333, Use: true},
	{Village: 10, Area: 1, Level: 0, XMin: 0, XMax: 1000, YMin: 50, YMax: 340, Use: true},
	{Village: 11, Area: 0, Level: 70, XMin: 67, XMax: 2580, YMin: 219, YMax: 333, Use: true},
	{Village: 11, Area: 1, Level: 70, XMin: 100, XMax: 1473, YMin: 206, YMax: 339, Use: true},
	{Village: 11, Area: 2, Level: 70, XMin: 67, XMax: 942, YMin: 230, YMax: 350, Use: true},
	{Village: 12, Area: 0, Level: 70, XMin: 95, XMax: 720, YMin: 206, YMax: 353, Use: true},
	{Village: 13, Area: 0, Level: 70, XMin: 116, XMax: 780, YMin: 206, YMax: 353, Use: true},
	{Village: 14, Area: 0, Level: 78, XMin: 85, XMax: 1407, YMin: 212, YMax: 341, Use: true},
	{Village: 14, Area: 1, Level: 78, XMin: 46, XMax: 1910, YMin: 216, YMax: 290, Use: true},
	{Village: 14, Area: 2, Level: 78, XMin: 37, XMax: 786, YMin: 200, YMax: 340, Use: true},
}

func applyReferenceTownCoordinates(items []mapCatalogItem) []mapCatalogItem {
	byKey := make(map[[2]int]int, len(items))
	for i := range items {
		byKey[[2]int{items[i].Village, items[i].Area}] = i
	}
	for _, ref := range referenceTownMaps {
		key := [2]int{ref.Village, ref.Area}
		if idx, ok := byKey[key]; ok {
			items[idx] = ref
			continue
		}
		byKey[key] = len(items)
		items = append(items, ref)
	}
	return items
}
