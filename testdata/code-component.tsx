import React from 'react';

export interface ButtonProps {
  variant?: 'primary' | 'secondary';
  onClick?: () => void;
}

export const Button: React.FC<ButtonProps> = ({ variant = 'primary', onClick }) => {
  return <button className={variant} onClick={onClick} />;
};

export function createButton(props: ButtonProps) {
  return <Button {...props} />;
}
