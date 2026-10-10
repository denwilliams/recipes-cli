import { ProviderRegistry } from './registry.js';
import { recipeTinEats } from './recipetin-eats.js';

// Register a second built-in provider here; CLI handlers remain unchanged.
export const registry = new ProviderRegistry([recipeTinEats]);
