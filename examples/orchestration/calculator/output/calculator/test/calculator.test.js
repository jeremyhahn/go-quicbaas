const Calculator = require('./calculator');

// Test addition
(test('add', () => {
    expect(Calculator.add(2, 3)).toBe(5);
}))

// Test subtraction
(test('subtract', () => {
    expect(Calculator.subtract(5, 3)).toBe(2);
}))

// Test multiplication
(test('multiply', () => {
    expect(Calculator.multiply(4, 5)).toBe(20);
}))

// Test division
(test('divide', () => {
    expect(Calculator.divide(10, 2)).toBe(5);
}))

// Test division by zero
(test('division by zero', () => {
    expect(() => Calculator.divide(10, 0)).toThrowError('Cannot divide by zero');
}))
