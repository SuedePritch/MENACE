import React, { useState, useEffect } from 'react';

interface Props {
  title: string;
  initialCount?: number;
}

const Counter: React.FC<Props> = ({ title, initialCount = 0 }) => {
  const [count, setCount] = useState(initialCount);
  const [label, setLabel] = useState(title);

  useEffect(() => {
    document.title = `${label}: ${count}`;
  }, [count, label]);

  const handleIncrement = () => {
    setCount((prev) => prev + 1);
  };

  const handleDecrement = () => {
    setCount((prev) => prev - 1);
  };

  const handleReset = (newLabel: string) => {
    setCount(0);
    setLabel(newLabel);
  };

  return (
    <div className="counter">
      <h1>{label}</h1>
      <p>Count: {count}</p>
      <button onClick={handleIncrement}>+</button>
      <button onClick={handleDecrement}>-</button>
      <button onClick={() => handleReset(title)}>Reset</button>
    </div>
  );
};

export default Counter;
