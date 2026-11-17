package service

import "math"

// GradeScale is the week grade scale, lowest first. A grade's number is its place in the list: red 1, yellow 2,
// green 3. This is the one place the scale is defined for averages; to grade 1-5 later, list five levels here.
var GradeScale = []string{"red", "yellow", "green"}

// GradeAverage is a student's average week grade.
type GradeAverage struct {
	Average     float64 // e.g. 2.4, rounded to one decimal
	Level       string  // the level the average rounds to, e.g. "yellow" (2.4 -> 2); .5 rounds up
	GradedWeeks int     // how many graded weeks the average is over
}

// gradeValue is a level's number on the scale (1 = lowest), or 0 for an unknown level.
func gradeValue(level string) int {
	for i, l := range GradeScale {
		if l == level {
			return i + 1
		}
	}
	return 0
}

// AverageGrade averages the given week grades on GradeScale. Unknown levels are skipped; nil when nothing is left.
func AverageGrade(levels []string) *GradeAverage {
	sum, n := 0, 0
	for _, l := range levels {
		if v := gradeValue(l); v > 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	avg := float64(sum) / float64(n)
	place := int(math.Floor(avg + 0.5)) // round half up: 2.4 -> 2, 2.5 -> 3
	if place < 1 {
		place = 1
	}
	if place > len(GradeScale) {
		place = len(GradeScale)
	}
	return &GradeAverage{Average: math.Round(avg*10) / 10, Level: GradeScale[place-1], GradedWeeks: n}
}
